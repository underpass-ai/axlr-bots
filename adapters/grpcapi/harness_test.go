package grpcapi

import (
	"context"
	"net"
	"path/filepath"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/types/known/durationpb"

	"github.com/underpass-ai/axlr-bots/adapters/pki"
	"github.com/underpass-ai/axlr-bots/adapters/unixapi"
	operatorv1 "github.com/underpass-ai/axlr-bots/api/gen/axlrbots/operator/v1"
	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/compose"
	"github.com/underpass-ai/axlr-bots/internal/fakeaxlr"
)

const testToken = "operator-token-for-tests"

// fixture is a control plane in process with the gRPC API served on a
// bufconn: authed carries the bearer token, anonymous none.
type fixture struct {
	ca        pki.Authority
	local     *unixapi.Local
	deps      application.Dependencies
	authed    *grpc.ClientConn
	anonymous *grpc.ClientConn
}

func newFixture(t *testing.T, scenario string, options ...Option) *fixture {
	t.Helper()
	return newFixtureWith(t, scenario, nil, options...)
}

// newFixtureWith is newFixture with a change to the dependencies (a test
// clock).
func newFixtureWith(t *testing.T, scenario string, change func(*application.Dependencies), options ...Option) *fixture {
	t.Helper()
	dir := t.TempDir()
	bin, _, err := fakeaxlr.Write(dir, fakeaxlr.Options{Scenario: scenario, Marker: filepath.Join(dir, "marker")})
	if err != nil {
		t.Fatal(err)
	}
	authority, err := pki.Generate(filepath.Join(dir, "state", pki.CADirName))
	if err != nil {
		t.Fatal(err)
	}
	deps, err := compose.Dependencies(filepath.Join(dir, "state"), bin, "test:grpc")
	if err != nil {
		t.Fatal(err)
	}
	if change != nil {
		change(&deps)
	}
	local := unixapi.NewLocal(deps)
	listener := bufconn.Listen(1 << 20)
	ctx, cancel := context.WithCancel(context.Background())
	api := New(local, options...)
	done := make(chan error, 1)
	go func() { done <- api.serve(ctx, listener, api.build(testToken), func() {}) }()
	t.Cleanup(func() {
		cancel()
		if err := <-done; err != nil {
			t.Error(err)
		}
	})
	dial := func(extra ...grpc.DialOption) *grpc.ClientConn {
		options := append([]grpc.DialOption{
			grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }),
			grpc.WithTransportCredentials(insecure.NewCredentials()),
		}, extra...)
		conn, err := grpc.NewClient("passthrough:///bufnet", options...)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = conn.Close() })
		return conn
	}
	bearer := grpc.WithUnaryInterceptor(func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		return invoker(metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+testToken), method, req, reply, cc, opts...)
	})
	bearerStream := grpc.WithStreamInterceptor(func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, streamer grpc.Streamer, opts ...grpc.CallOption) (grpc.ClientStream, error) {
		return streamer(metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+testToken), desc, cc, method, opts...)
	})
	return &fixture{ca: authority, local: local, deps: local.Dependencies(), authed: dial(bearer, bearerStream), anonymous: dial()}
}

// definition is a bot the services accept.
func definition(name, level string) *operatorv1.BotDefinition {
	return &operatorv1.BotDefinition{
		Tenant: "ops", Name: name, Owner: "operator", Mission: "do what each task asks, within the policy",
		AllowedTools: []string{"local_read", "local_exec"}, Level: level,
		ToolRules: &operatorv1.ToolRules{Allow: []*operatorv1.ToolRule{{Tool: "local_exec", Arguments: map[string]string{"program": "journalctl"}}}},
		Model:     &operatorv1.ModelPolicy{Model: "anthropic/claude-haiku-5.5"},
		Budgets:   &operatorv1.Budgets{TaskTimeout: durationpb.New(time.Minute)},
	}
}

// kind is the ErrorDetail kind of an error, and its code.
func kind(t *testing.T, err error) (string, string) {
	t.Helper()
	st, ok := status.FromError(err)
	if !ok {
		t.Fatalf("not a status: %v", err)
	}
	for _, detail := range st.Details() {
		if d, ok := detail.(*operatorv1.ErrorDetail); ok {
			return d.GetKind(), st.Code().String()
		}
	}
	return "", st.Code().String()
}

func registerAndEnable(t *testing.T, f *fixture, name, level string) {
	t.Helper()
	bots := operatorv1.NewBotsServiceClient(f.authed)
	if _, err := bots.RegisterBot(context.Background(), &operatorv1.RegisterBotRequest{Definition: definition(name, level)}); err != nil {
		t.Fatal(err)
	}
	if _, err := bots.EnableBot(context.Background(), &operatorv1.EnableBotRequest{BotId: name}); err != nil {
		t.Fatal(err)
	}
}

func errorDetail(t *testing.T, err error) *operatorv1.ErrorDetail {
	t.Helper()
	st, _ := status.FromError(err)
	for _, detail := range st.Details() {
		if d, ok := detail.(*operatorv1.ErrorDetail); ok {
			return d
		}
	}
	t.Fatalf("no ErrorDetail in %v", err)
	return nil
}
