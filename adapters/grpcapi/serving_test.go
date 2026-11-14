package grpcapi

import (
	"context"
	"crypto/tls"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"

	"github.com/underpass-ai/axlr-bots/adapters/filestore"
	"github.com/underpass-ai/axlr-bots/adapters/pki"
	operatorv1 "github.com/underpass-ai/axlr-bots/api/gen/axlrbots/operator/v1"
)

func TestServeTLSTakesTheFleetCAAnOperatorCertificateAndTheToken(t *testing.T) {
	f := newFixture(t, "completed")
	api := New(f.local, WithOperatorCertificates(pki.NewOperatorCertificates(filestore.NewCertificateDenylist(t.TempDir()))))
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- api.ServeTLS(ctx, listener, pki.ServerTLSConfig(f.ca, []string{"127.0.0.1"}), testToken)
	}()

	clientConfig, err := pki.EnrollTLSConfig(f.ca.CertificatePEM())
	if err != nil {
		t.Fatal(err)
	}
	operator, _ := operatorCertificate(t, f.ca, "alice")
	clientConfig.Certificates = []tls.Certificate{operator}
	conn, err := grpc.NewClient(listener.Addr().String(), grpc.WithTransportCredentials(credentials.NewTLS(clientConfig.Clone())))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	bots := operatorv1.NewBotsServiceClient(conn)
	callCtx, stop := context.WithTimeout(context.Background(), 10*time.Second)
	defer stop()
	if _, err := bots.ListBots(callCtx, &operatorv1.ListBotsRequest{}); err == nil {
		t.Error("a call with no token was served")
	} else if k, c := kind(t, err); k != "unauthorized" || c != "Unauthenticated" {
		t.Errorf("no token: %s %s", k, c)
	}
	authed := metadata.AppendToOutgoingContext(callCtx, "authorization", "Bearer "+testToken)
	if _, err := bots.ListBots(authed, &operatorv1.ListBotsRequest{}); err != nil {
		t.Errorf("the token: %v", err)
	}

	plain, err := grpc.NewClient(listener.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	defer plain.Close()
	if _, err := operatorv1.NewBotsServiceClient(plain).ListBots(authed, &operatorv1.ListBotsRequest{}); err == nil {
		t.Error("a call without TLS was served")
	}
	cancel()
	if err := <-done; err != nil {
		t.Error(err)
	}

	if err := api.ServeTLS(context.Background(), listener, nil, ""); err == nil {
		t.Error("served the operator address with no token")
	}
}

func TestServeSocketServesTheLocalUserWithNoToken(t *testing.T) {
	f := newFixture(t, "completed")
	socket := filepath.Join(t.TempDir(), "run", "cp.sock.grpc")
	if err := os.MkdirAll(filepath.Dir(socket), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(socket, nil, 0o600); err != nil {
		t.Fatal(err) // a socket file a dead daemon left
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- New(f.local).ServeSocket(ctx, socket) }()
	conn, err := grpc.NewClient("unix://"+socket, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	callCtx, stop := context.WithTimeout(ctx, 10*time.Second)
	defer stop()
	if _, err := operatorv1.NewBotsServiceClient(conn).ListBots(callCtx, &operatorv1.ListBotsRequest{}, grpc.WaitForReady(true)); err != nil {
		t.Fatalf("the socket should serve with no token: %v", err)
	}
	if info, err := os.Stat(socket); err != nil || info.Mode().Perm() != 0o660 {
		t.Errorf("socket: %v %v", info, err)
	}
	cancel()
	if err := <-done; err != nil {
		t.Error(err)
	}
	if _, err := os.Stat(socket); err == nil {
		t.Error("socket left behind")
	}

	file := filepath.Join(t.TempDir(), "file")
	_ = os.WriteFile(file, nil, 0o600)
	if err := New(f.local).ServeSocket(context.Background(), filepath.Join(file, "x.sock")); err == nil {
		t.Error("socket under a file served")
	}
}

// A watch never ends on its own: stopping the server cuts it after the grace.
func TestStoppingTheServerEndsOpenWatches(t *testing.T) {
	f := newFixture(t, "completed")
	registerAndEnable(t, f, "bot-w", "observe")
	socket := filepath.Join(t.TempDir(), "w.sock")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- New(f.local).ServeSocket(ctx, socket) }()
	conn, err := grpc.NewClient("unix://"+socket, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	stream, err := operatorv1.NewBotsServiceClient(conn).WatchBotEvents(context.Background(), &operatorv1.WatchBotEventsRequest{BotId: "bot-w"}, grpc.WaitForReady(true))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := stream.Recv(); err != nil {
		t.Fatal(err)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Error(err)
		}
	case <-time.After(shutdownGrace + 5*time.Second):
		t.Fatal("the server did not stop with a watch open")
	}
	for {
		if _, err := stream.Recv(); err != nil {
			break // the replayed events, then the end
		}
	}
}
