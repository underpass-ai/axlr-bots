package grpcapi

import (
	"context"
	"crypto/tls"
	"net"
	"path/filepath"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"

	"github.com/underpass-ai/axlr-bots/adapters/filestore"
	"github.com/underpass-ai/axlr-bots/adapters/pki"
	operatorv1 "github.com/underpass-ai/axlr-bots/api/gen/axlrbots/operator/v1"
	"github.com/underpass-ai/axlr-bots/domain"
)

// mutualFixture serves the TLS operator address of a control plane in
// process, with the policy the daemon wires, and builds clients with any
// certificate.
type mutualFixture struct {
	*fixture
	address  string
	denylist filestore.CertificateDenylist
	state    string
}

func newMutualFixture(t *testing.T) *mutualFixture {
	t.Helper()
	f := newFixture(t, "completed")
	state := t.TempDir()
	denylist := filestore.NewCertificateDenylist(state)
	api := New(f.local, WithOperatorCertificates(pki.NewOperatorCertificates(denylist)))
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- api.ServeTLS(ctx, listener, pki.ServerTLSConfig(f.ca, []string{"127.0.0.1"}), testToken)
	}()
	t.Cleanup(func() {
		cancel()
		if err := <-done; err != nil {
			t.Error(err)
		}
	})
	return &mutualFixture{fixture: f, address: listener.Addr().String(), denylist: denylist, state: state}
}

// call lists the bots with the certificate (none when nil) and the
// bearer token (none when empty), as the actor when one is given.
func (m *mutualFixture) call(t *testing.T, certificate *tls.Certificate, token, actor string) error {
	t.Helper()
	config, err := pki.EnrollTLSConfig(m.ca.CertificatePEM())
	if err != nil {
		t.Fatal(err)
	}
	if certificate != nil {
		config.Certificates = []tls.Certificate{*certificate}
	}
	conn, err := grpc.NewClient(m.address, grpc.WithTransportCredentials(credentials.NewTLS(config)))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if token != "" {
		ctx = metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+token)
	}
	if actor != "" {
		ctx = metadata.AppendToOutgoingContext(ctx, ActorMetadata, actor)
	}
	_, err = operatorv1.NewBotsServiceClient(conn).ListBots(ctx, &operatorv1.ListBotsRequest{})
	return err
}

func operatorCertificate(t *testing.T, authority pki.Authority, name string) (tls.Certificate, string) {
	t.Helper()
	keyPEM, certPEM, issued, err := authority.IssueOperator(name, 0)
	if err != nil {
		t.Fatal(err)
	}
	certificate, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatal(err)
	}
	return certificate, issued.Serial
}

func TestTheOperatorAddressRequiresAnOperatorCertificate(t *testing.T) {
	m := newMutualFixture(t)
	operator, _ := operatorCertificate(t, m.ca, "alice")

	if err := m.call(t, &operator, testToken, ""); err != nil {
		t.Errorf("an operator's certificate and the token: %v", err)
	}
	if err := m.call(t, &operator, testToken, "alice"); err != nil {
		t.Errorf("the actor that is the certificate's: %v", err)
	}
	if err := m.call(t, &operator, "", ""); err == nil {
		t.Error("the certificate alone was served: the token is the second factor")
	} else if k, c := kind(t, err); k != "unauthorized" || c != "Unauthenticated" {
		t.Errorf("no token: %s %s", k, c)
	}
	if err := m.call(t, nil, testToken, ""); err == nil {
		t.Error("a call with no client certificate was served")
	}
	if err := m.call(t, &operator, testToken, "mallory"); err == nil {
		t.Error("an actor that is not the certificate's was served")
	} else if k, c := kind(t, err); k != "forbidden" || c != "PermissionDenied" {
		t.Errorf("another actor: %s %s", k, c)
	}
}

func TestACertificateThatIsNotAnOperatorsIsRefused(t *testing.T) {
	m := newMutualFixture(t)

	nodeKey, csr, err := pki.NewNodeKeyAndRequest(domain.NodeID("web-1"))
	if err != nil {
		t.Fatal(err)
	}
	issuedNode, err := m.ca.IssueNode(csr, domain.NodeID("web-1"), 0)
	if err != nil {
		t.Fatal(err)
	}
	node, err := tls.X509KeyPair(issuedNode.CertificatePEM, nodeKey)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.call(t, &node, testToken, ""); err == nil {
		t.Error("a node's certificate was served as an operator's")
	} else if k, c := kind(t, err); k != "forbidden" || c != "PermissionDenied" {
		t.Errorf("node: %s %s", k, c)
	}

	serviceKey, serviceCert, err := m.ca.IssueClient("alice", 0)
	if err != nil {
		t.Fatal(err)
	}
	service, err := tls.X509KeyPair(serviceCert, serviceKey)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.call(t, &service, testToken, ""); err == nil {
		t.Error("a service's certificate, named like an operator, was served")
	}

	other, err := pki.Generate(filepath.Join(t.TempDir(), "other-ca"))
	if err != nil {
		t.Fatal(err)
	}
	stranger, _ := operatorCertificate(t, other, "alice")
	if err := m.call(t, &stranger, testToken, ""); err == nil {
		t.Error("an operator certificate of another CA was served")
	}
}

func TestARevokedOperatorCertificateIsRefusedAtOnce(t *testing.T) {
	m := newMutualFixture(t)
	operator, serial := operatorCertificate(t, m.ca, "alice")
	if err := m.call(t, &operator, testToken, ""); err != nil {
		t.Fatal(err)
	}
	if err := m.denylist.Deny(context.Background(), serial, "operator:alice"); err != nil {
		t.Fatal(err)
	}
	err := m.call(t, &operator, testToken, "")
	if err == nil {
		t.Fatal("a revoked certificate was served")
	}
	if k, c := kind(t, err); k != "forbidden" || c != "PermissionDenied" || !errorDetail(t, err).GetRevoked() {
		t.Errorf("revoked: %s %s", k, c)
	}
	another, _ := operatorCertificate(t, m.ca, "alice")
	if err := m.call(t, &another, testToken, ""); err != nil {
		t.Errorf("a new certificate of the same operator: %v", err)
	}
}

func TestServeTLSRefusesToServeWithoutThePolicy(t *testing.T) {
	f := newFixture(t, "completed")
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	config := pki.ServerTLSConfig(f.ca, []string{"127.0.0.1"})
	if err := New(f.local).ServeTLS(context.Background(), listener, config, testToken); err == nil {
		t.Error("served the operator address with no operator certificates policy")
	}
	api := New(f.local, WithOperatorCertificates(pki.NewOperatorCertificates(filestore.NewCertificateDenylist(t.TempDir()))))
	if err := api.ServeTLS(context.Background(), listener, nil, testToken); err == nil {
		t.Error("served the operator address with no TLS configuration")
	}
}

func TestCertificateInterceptorRefusesACallWithoutTLS(t *testing.T) {
	interceptor := certificateInterceptor{operators: pki.NewOperatorCertificates(filestore.NewCertificateDenylist(t.TempDir()))}
	handler := func(context.Context, any) (any, error) { t.Error("the handler ran"); return nil, nil }
	if _, err := interceptor.unary(context.Background(), nil, &grpc.UnaryServerInfo{}, handler); err == nil {
		t.Error("unary: served a call with no peer")
	}
	if err := interceptor.stream(nil, wrappedStream{ctx: context.Background()}, &grpc.StreamServerInfo{}, func(any, grpc.ServerStream) error { t.Error("the handler ran"); return nil }); err == nil {
		t.Error("stream: served a call with no peer")
	}
}
