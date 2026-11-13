package proto_test

import (
	"context"
	"net"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	protobuf "google.golang.org/protobuf/proto"

	operatorv1 "github.com/underpass-ai/axlr-bots/api/gen/axlrbots/operator/v1"
)

// connection serves every service of the contract unimplemented over an
// in-process listener and returns a client connection to it.
func connection(t *testing.T) *grpc.ClientConn {
	t.Helper()
	listener := bufconn.Listen(1 << 20)
	server := grpc.NewServer()
	operatorv1.RegisterBotsServiceServer(server, operatorv1.UnimplementedBotsServiceServer{})
	operatorv1.RegisterTasksServiceServer(server, operatorv1.UnimplementedTasksServiceServer{})
	operatorv1.RegisterExecutionsServiceServer(server, operatorv1.UnimplementedExecutionsServiceServer{})
	operatorv1.RegisterNodesServiceServer(server, operatorv1.UnimplementedNodesServiceServer{})
	operatorv1.RegisterGroupsServiceServer(server, operatorv1.UnimplementedGroupsServiceServer{})
	operatorv1.RegisterOverviewServiceServer(server, operatorv1.UnimplementedOverviewServiceServer{})
	operatorv1.RegisterActivityServiceServer(server, operatorv1.UnimplementedActivityServiceServer{})
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)
	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

func unimplemented(t *testing.T, err error) {
	t.Helper()
	if status.Code(err) != codes.Unimplemented {
		t.Fatalf("code = %v (%v), want Unimplemented", status.Code(err), err)
	}
}

func TestEveryServiceAnswersUnimplemented(t *testing.T) {
	conn, ctx := connection(t), context.Background()

	_, err := operatorv1.NewBotsServiceClient(conn).ListBots(ctx, &operatorv1.ListBotsRequest{})
	unimplemented(t, err)
	_, err = operatorv1.NewTasksServiceClient(conn).GetTask(ctx, &operatorv1.GetTaskRequest{TaskId: "task-1"})
	unimplemented(t, err)
	_, err = operatorv1.NewExecutionsServiceClient(conn).GetExecution(ctx, &operatorv1.GetExecutionRequest{ExecutionId: "exec-1"})
	unimplemented(t, err)
	_, err = operatorv1.NewNodesServiceClient(conn).ListNodes(ctx, &operatorv1.ListNodesRequest{})
	unimplemented(t, err)
	_, err = operatorv1.NewGroupsServiceClient(conn).ListGroups(ctx, &operatorv1.ListGroupsRequest{})
	unimplemented(t, err)
	_, err = operatorv1.NewOverviewServiceClient(conn).GetOverview(ctx, &operatorv1.GetOverviewRequest{})
	unimplemented(t, err)
	_, err = operatorv1.NewActivityServiceClient(conn).GetTaskActivity(ctx, &operatorv1.GetTaskActivityRequest{TaskId: "task-1"})
	unimplemented(t, err)
}

func TestStreamingRPCsAnswerUnimplemented(t *testing.T) {
	conn, ctx := connection(t), context.Background()

	watch, err := operatorv1.NewActivityServiceClient(conn).WatchTaskActivity(ctx, &operatorv1.WatchTaskActivityRequest{TaskId: "task-1"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = watch.Recv()
	unimplemented(t, err)

	download, err := operatorv1.NewExecutionsServiceClient(conn).GetArtifact(ctx, &operatorv1.GetArtifactRequest{ExecutionId: "exec-1", Name: "out.txt"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = download.Recv()
	unimplemented(t, err)
}

// A patch tells an absent list from an empty one: the wire keeps the
// difference, which a repeated field could not.
func TestBotPatchKeepsAbsentApartFromEmpty(t *testing.T) {
	roundTrip := func(patch *operatorv1.BotPatch) *operatorv1.BotPatch {
		raw, err := protobuf.Marshal(patch)
		if err != nil {
			t.Fatal(err)
		}
		out := &operatorv1.BotPatch{}
		if err := protobuf.Unmarshal(raw, out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	if got := roundTrip(&operatorv1.BotPatch{}); got.AllowedTools != nil || got.Mission != nil {
		t.Fatalf("an empty patch changes %v", got)
	}
	cleared := roundTrip(&operatorv1.BotPatch{AllowedTools: &operatorv1.StringList{}, Mission: protobuf.String("")})
	if cleared.AllowedTools == nil || cleared.Mission == nil {
		t.Fatalf("a cleared field was lost: %v", cleared)
	}
}
