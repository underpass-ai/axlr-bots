package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	"github.com/underpass-ai/axlr-bots/adapters/pki"
	operatorv1 "github.com/underpass-ai/axlr-bots/api/gen/axlrbots/operator/v1"
)

type fakeBotsService struct {
	operatorv1.UnimplementedBotsServiceServer
}
type fakeTasksService struct {
	operatorv1.UnimplementedTasksServiceServer
}
type fakeOverviewService struct {
	operatorv1.UnimplementedOverviewServiceServer
}

// fakeOperatorAPI is the control plane's operator API as the command
// sees it: canned answers by method, and the metadata of what arrived.
type fakeOperatorAPI struct {
	operatorv1.UnimplementedActivityServiceServer

	mu       sync.Mutex
	canned   map[string]proto.Message
	failures map[string]error
	headers  map[string]metadata.MD
	watch    []*operatorv1.WatchTaskActivityResponse
	token    string
	// certFile and keyFile are an operator's certificate the fake accepts
	// (serveTLS), and operators the names the
	// calls' client certificates carried.
	certFile, keyFile string
	operators         []string
}

func newFakeOperatorAPI() *fakeOperatorAPI {
	return &fakeOperatorAPI{canned: map[string]proto.Message{}, failures: map[string]error{}, headers: map[string]metadata.MD{}}
}

func apiMethod(service, name string) string { return "/axlrbots.operator.v1." + service + "/" + name }

func (f *fakeOperatorAPI) answer(method string, response proto.Message) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.canned[method] = response
}

func (f *fakeOperatorAPI) fail(method string, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failures[method] = err
}

func (f *fakeOperatorAPI) seen(method string) metadata.MD {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.headers[method]
}

func (f *fakeOperatorAPI) unary(ctx context.Context, _ any, info *grpc.UnaryServerInfo, _ grpc.UnaryHandler) (any, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	md, _ := metadata.FromIncomingContext(ctx)
	f.headers[info.FullMethod] = md
	if info, ok := peer.FromContext(ctx); ok {
		if tlsInfo, ok := info.AuthInfo.(credentials.TLSInfo); ok && len(tlsInfo.State.PeerCertificates) > 0 {
			if name, ok := pki.OperatorName(tlsInfo.State.PeerCertificates[0]); ok {
				f.operators = append(f.operators, name)
			} else {
				return nil, status.Error(codes.PermissionDenied, "not an operator's certificate")
			}
		}
	}
	if f.token != "" && (len(md.Get("authorization")) != 1 || md.Get("authorization")[0] != "Bearer "+f.token) {
		return nil, status.Error(codes.Unauthenticated, "bad token")
	}
	if err := f.failures[info.FullMethod]; err != nil {
		return nil, err
	}
	if response, ok := f.canned[info.FullMethod]; ok {
		return response, nil
	}
	return nil, status.Error(codes.Unimplemented, "nothing canned")
}

func (f *fakeOperatorAPI) WatchTaskActivity(_ *operatorv1.WatchTaskActivityRequest, stream operatorv1.ActivityService_WatchTaskActivityServer) error {
	for _, message := range f.watch {
		if err := stream.Send(message); err != nil {
			return err
		}
	}
	return nil
}

func (f *fakeOperatorAPI) server(options ...grpc.ServerOption) *grpc.Server {
	server := grpc.NewServer(append(options, grpc.UnaryInterceptor(f.unary))...)
	operatorv1.RegisterBotsServiceServer(server, fakeBotsService{})
	operatorv1.RegisterTasksServiceServer(server, fakeTasksService{})
	operatorv1.RegisterOverviewServiceServer(server, fakeOverviewService{})
	operatorv1.RegisterActivityServiceServer(server, f)
	return server
}

// serveSocket serves the fake on a Unix socket and returns its path.
func (f *fakeOperatorAPI) serveSocket(t *testing.T) string {
	t.Helper()
	socket := filepath.Join(t.TempDir(), "grpc.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	server := f.server()
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)
	return socket
}

// serveTLS serves the fake over TLS on the loopback behind the token, and
// returns the address, the token file and the CA file the command takes.
func (f *fakeOperatorAPI) serveTLS(t *testing.T) (address, tokenFile, caFile string) {
	t.Helper()
	caKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	caTemplate := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "test CA"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	caDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	caCert, _ := x509.ParseCertificate(caDER)
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	template := &x509.Certificate{SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: "cp"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, template, caCert, &key.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	f.token = "remote-token"
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(caCert)
	server := f.server(grpc.Creds(credentials.NewTLS(&tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}, ClientAuth: tls.RequireAndVerifyClientCert, ClientCAs: pool})))
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)
	dir := t.TempDir()
	tokenFile = filepath.Join(dir, "token")
	caFile = filepath.Join(dir, "ca.pem")
	if err := os.WriteFile(tokenFile, []byte("remote-token\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(caFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER}), 0o600); err != nil {
		t.Fatal(err)
	}
	caKeyDER, _ := x509.MarshalPKCS8PrivateKey(caKey)
	authority, err := pki.Parse(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER}), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: caKeyDER}))
	if err != nil {
		t.Fatal(err)
	}
	keyPEM, certPEM, _, err := authority.IssueOperator("alice", 0)
	if err != nil {
		t.Fatal(err)
	}
	f.certFile, f.keyFile = filepath.Join(dir, operatorCertFile), filepath.Join(dir, operatorKeyFile)
	if err := os.WriteFile(f.certFile, certPEM, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f.keyFile, keyPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	return listener.Addr().String(), tokenFile, caFile
}
