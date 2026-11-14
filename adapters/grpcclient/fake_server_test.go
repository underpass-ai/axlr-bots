package grpcclient

import (
	"context"
	"net"
	"sync"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/proto"

	operatorv1 "github.com/underpass-ai/axlr-bots/api/gen/axlrbots/operator/v1"
)

// Every service of the contract, embedded unimplemented: what a test
// answers it answers from the canned table of the interceptor, and the
// three streams have handlers of their own.
type fakeBots struct {
	operatorv1.UnimplementedBotsServiceServer
}
type fakeGroups struct {
	operatorv1.UnimplementedGroupsServiceServer
}
type fakeTasks struct {
	operatorv1.UnimplementedTasksServiceServer
}
type fakeNodes struct {
	operatorv1.UnimplementedNodesServiceServer
}
type fakeOverview struct {
	operatorv1.UnimplementedOverviewServiceServer
}

// fakeControlPlane answers the operator API from canned messages and
// records what reached it.
type fakeControlPlane struct {
	operatorv1.UnimplementedExecutionsServiceServer
	operatorv1.UnimplementedActivityServiceServer

	mu       sync.Mutex
	canned   map[string]proto.Message
	failures map[string]error
	requests map[string]proto.Message
	metadata map[string]metadata.MD

	artifact   []*operatorv1.GetArtifactResponse
	transcript []*operatorv1.GetTranscriptResponse
	watch      []*operatorv1.WatchTaskActivityResponse
	watchError error
	cancelled  chan struct{}
	// token, when set, is what every unary call must carry.
	token string
}

func (f *fakeControlPlane) answer(method string, response proto.Message) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.canned[method] = response
}

func (f *fakeControlPlane) fail(method string, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failures[method] = err
}

func (f *fakeControlPlane) request(method string) proto.Message {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.requests[method]
}

func (f *fakeControlPlane) headers(method string) metadata.MD {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.metadata[method]
}

func (f *fakeControlPlane) record(ctx context.Context, method string, request any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if incoming, ok := metadata.FromIncomingContext(ctx); ok {
		f.metadata[method] = incoming
	}
	if message, ok := request.(proto.Message); ok {
		f.requests[method] = message
	}
}

func (f *fakeControlPlane) unary(ctx context.Context, request any, info *grpc.UnaryServerInfo, _ grpc.UnaryHandler) (any, error) {
	f.record(ctx, info.FullMethod, request)
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.token != "" && incomingAuthorization(ctx) != "Bearer "+f.token {
		return nil, status.Error(codes.Unauthenticated, "bad token")
	}
	if err := f.failures[info.FullMethod]; err != nil {
		return nil, err
	}
	if response, ok := f.canned[info.FullMethod]; ok {
		return response, nil
	}
	return nil, status.Error(12, "nothing canned for "+info.FullMethod)
}

func (f *fakeControlPlane) stream(srv any, stream grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
	f.record(stream.Context(), info.FullMethod, nil)
	return handler(srv, stream)
}

func (f *fakeControlPlane) GetArtifact(request *operatorv1.GetArtifactRequest, stream operatorv1.ExecutionsService_GetArtifactServer) error {
	f.record(stream.Context(), "GetArtifact", request)
	if err := f.failures["GetArtifact"]; err != nil {
		return err
	}
	for _, message := range f.artifact {
		if err := stream.Send(message); err != nil {
			return err
		}
	}
	if f.cancelled != nil {
		<-stream.Context().Done()
		close(f.cancelled)
	}
	return nil
}

func (f *fakeControlPlane) GetTranscript(request *operatorv1.GetTranscriptRequest, stream operatorv1.ExecutionsService_GetTranscriptServer) error {
	f.record(stream.Context(), "GetTranscript", request)
	if err := f.failures["GetTranscript"]; err != nil {
		return err
	}
	for _, message := range f.transcript {
		if err := stream.Send(message); err != nil {
			return err
		}
	}
	return nil
}

func (f *fakeControlPlane) WatchTaskActivity(request *operatorv1.WatchTaskActivityRequest, stream operatorv1.ActivityService_WatchTaskActivityServer) error {
	f.record(stream.Context(), "WatchTaskActivity", request)
	if err := f.failures["WatchTaskActivity"]; err != nil {
		return err
	}
	for _, message := range f.watch {
		if err := stream.Send(message); err != nil {
			return err
		}
	}
	return f.watchError
}

// serve registers the fakes on a server and returns it, not started.
func (f *fakeControlPlane) server(options ...grpc.ServerOption) *grpc.Server {
	options = append(options, grpc.UnaryInterceptor(f.unary), grpc.StreamInterceptor(f.stream), grpc.MaxSendMsgSize(64<<20))
	server := grpc.NewServer(options...)
	operatorv1.RegisterBotsServiceServer(server, fakeBots{})
	operatorv1.RegisterGroupsServiceServer(server, fakeGroups{})
	operatorv1.RegisterTasksServiceServer(server, fakeTasks{})
	operatorv1.RegisterNodesServiceServer(server, fakeNodes{})
	operatorv1.RegisterOverviewServiceServer(server, fakeOverview{})
	operatorv1.RegisterExecutionsServiceServer(server, f)
	operatorv1.RegisterActivityServiceServer(server, f)
	return server
}

func newFake() *fakeControlPlane {
	return &fakeControlPlane{canned: map[string]proto.Message{}, failures: map[string]error{}, requests: map[string]proto.Message{}, metadata: map[string]metadata.MD{}}
}

// harness is a client over an in-process connection to a fake control
// plane that wants the token.
type harness struct {
	fake   *fakeControlPlane
	client *Client
}

const testToken = "operator-token"

func newHarness(t *testing.T) *harness {
	t.Helper()
	fake := newFake()
	listener := bufconn.Listen(1 << 20)
	server := fake.server()
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)
	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return &harness{fake: fake, client: New(conn, "bufnet", testToken)}
}

// method is the full name of an RPC of the operator API.
func method(service, name string) string {
	return "/axlrbots.operator.v1." + service + "/" + name
}
