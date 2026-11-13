package daemon

import (
	"context"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"

	"github.com/underpass-ai/axlr-bots/adapters/pki"
	operatorv1 "github.com/underpass-ai/axlr-bots/api/gen/axlrbots/operator/v1"
)

func TestDaemonServesTheOperatorAPIOverGRPC(t *testing.T) {
	f := newFixture(t, "success")
	ca, err := pki.Generate(filepath.Join(f.state, pki.CADirName))
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	metricsListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	_, stop := f.startWith(t, func(o *Options) {
		o.GRPCListener, o.APIToken, o.TLS = listener, "operator-token", pki.ServerTLSConfig(ca, []string{"127.0.0.1"})
		o.MetricsListener = metricsListener
	})
	defer stop()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	// On the local socket, beside the HTTP one: no token.
	local, err := grpc.NewClient("unix://"+filepath.Join(filepath.Dir(f.socket), "axlr-bots-grpc.sock"), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	defer local.Close()
	if _, err := operatorv1.NewBotsServiceClient(local).ListBots(ctx, &operatorv1.ListBotsRequest{}, grpc.WaitForReady(true)); err != nil {
		t.Errorf("over the socket: %v", err)
	}

	// On the TLS address: the fleet CA and the bearer token.
	clientConfig, _ := pki.EnrollTLSConfig(ca.CertificatePEM())
	remote, err := grpc.NewClient(listener.Addr().String(), grpc.WithTransportCredentials(credentials.NewTLS(clientConfig.Clone())))
	if err != nil {
		t.Fatal(err)
	}
	defer remote.Close()
	bots := operatorv1.NewBotsServiceClient(remote)
	if _, err := bots.ListBots(ctx, &operatorv1.ListBotsRequest{}); err == nil {
		t.Error("a call with no token was served")
	}
	if _, err := bots.ListBots(metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer operator-token"), &operatorv1.ListBotsRequest{}); err != nil {
		t.Errorf("over TLS with the token: %v", err)
	}

	body := get(t, "http://"+metricsListener.Addr().String()+"/metrics")
	for _, want := range []string{
		`axlr_bots_grpc_requests_total{code="OK",method="ListBots",service="axlrbots.operator.v1.BotsService"} 2`,
		`axlr_bots_grpc_requests_total{code="Unauthenticated",method="ListBots",service="axlrbots.operator.v1.BotsService"} 1`,
		`axlr_bots_grpc_request_duration_seconds_count{code="OK",method="ListBots",service="axlrbots.operator.v1.BotsService"} 2`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("metrics lack %s:\n%s", want, body)
		}
	}
	if !strings.Contains(f.logs.String(), "grpc://127.0.0.1:") {
		t.Errorf("the start line does not name the gRPC address:\n%s", f.logs.String())
	}
}

func TestDaemonGRPCAddressOptions(t *testing.T) {
	f := newFixture(t, "completed")
	if err := Run(context.Background(), f.deps, Options{StateDir: f.state, SocketPath: f.socket, GRPCListenAddr: "127.0.0.1:0", APIToken: "t"}); err == nil || !strings.Contains(err.Error(), "TLS configuration") {
		t.Errorf("gRPC address without TLS: %v", err)
	}
	ca, _ := pki.Generate(filepath.Join(f.state, pki.CADirName))
	tlsConfig := pki.ServerTLSConfig(ca, []string{"127.0.0.1"})
	if err := Run(context.Background(), f.deps, Options{StateDir: f.state, SocketPath: f.socket, GRPCListenAddr: "127.0.0.1:0", TLS: tlsConfig}); err == nil || !strings.Contains(err.Error(), "bearer token") {
		t.Errorf("gRPC address without a token: %v", err)
	}
	if err := Run(context.Background(), f.deps, Options{StateDir: f.state, SocketPath: f.socket, GRPCListenAddr: "300.0.0.1:0", APIToken: "t", TLS: tlsConfig}); err == nil {
		t.Error("a bad gRPC address listened")
	}
	if (Options{SocketPath: "/run/cp.sock"}).grpcSocketPath() != "/run/axlr-bots-grpc.sock" || (Options{GRPCSocketPath: "/x"}).grpcSocketPath() != "/x" {
		t.Error("gRPC socket path")
	}
}
