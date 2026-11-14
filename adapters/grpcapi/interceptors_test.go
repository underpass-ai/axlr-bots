package grpcapi

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/underpass-ai/axlr-bots/adapters/unixapi"
	operatorv1 "github.com/underpass-ai/axlr-bots/api/gen/axlrbots/operator/v1"
	"github.com/underpass-ai/axlr-bots/domain"
)

// Every service refuses a call with no token, and a wrong one, as
// UNAUTHENTICATED with the unauthorized kind.
func TestEveryServiceRefusesACallWithoutTheToken(t *testing.T) {
	f := newFixture(t, "completed")
	ctx := context.Background()
	wrong := metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer not-the-token")
	basic := metadata.AppendToOutgoingContext(ctx, "authorization", "Basic "+testToken)
	calls := map[string]func(context.Context) error{
		"bots": func(ctx context.Context) error {
			_, err := operatorv1.NewBotsServiceClient(f.anonymous).ListBots(ctx, &operatorv1.ListBotsRequest{})
			return err
		},
		"tasks": func(ctx context.Context) error {
			_, err := operatorv1.NewTasksServiceClient(f.anonymous).ListTasks(ctx, &operatorv1.ListTasksRequest{})
			return err
		},
		"groups": func(ctx context.Context) error {
			_, err := operatorv1.NewGroupsServiceClient(f.anonymous).ListGroups(ctx, &operatorv1.ListGroupsRequest{})
			return err
		},
		"executions": func(ctx context.Context) error {
			_, err := operatorv1.NewExecutionsServiceClient(f.anonymous).ListTranscripts(ctx, &operatorv1.ListTranscriptsRequest{})
			return err
		},
		"nodes": func(ctx context.Context) error {
			_, err := operatorv1.NewNodesServiceClient(f.anonymous).ListNodes(ctx, &operatorv1.ListNodesRequest{})
			return err
		},
		"overview": func(ctx context.Context) error {
			_, err := operatorv1.NewOverviewServiceClient(f.anonymous).GetBuild(ctx, &operatorv1.GetBuildRequest{})
			return err
		},
		"activity": func(ctx context.Context) error {
			_, err := operatorv1.NewActivityServiceClient(f.anonymous).GetTaskActivity(ctx, &operatorv1.GetTaskActivityRequest{})
			return err
		},
		"stream": func(ctx context.Context) error {
			stream, err := operatorv1.NewBotsServiceClient(f.anonymous).WatchBotEvents(ctx, &operatorv1.WatchBotEventsRequest{BotId: "x"})
			if err != nil {
				return err
			}
			_, err = stream.Recv()
			return err
		},
		"download": func(ctx context.Context) error {
			stream, err := operatorv1.NewExecutionsServiceClient(f.anonymous).GetArtifact(ctx, &operatorv1.GetArtifactRequest{ExecutionId: "x", Name: "y"})
			if err != nil {
				return err
			}
			_, err = stream.Recv()
			return err
		},
	}
	for name, call := range calls {
		for label, c := range map[string]context.Context{"no token": ctx, "wrong token": wrong, "wrong scheme": basic} {
			err := call(c)
			if k, code := kind(t, err); k != "unauthorized" || code != "Unauthenticated" {
				t.Errorf("%s with %s: %s %s (%v)", name, label, k, code, err)
			}
		}
	}
	if _, err := operatorv1.NewBotsServiceClient(f.authed).ListBots(ctx, &operatorv1.ListBotsRequest{}); err != nil {
		t.Errorf("the right token: %v", err)
	}
}

func TestTheActivityServiceIsUnimplementedUntilOneIsPlugged(t *testing.T) {
	f := newFixture(t, "completed")
	_, err := operatorv1.NewActivityServiceClient(f.authed).GetTaskActivity(context.Background(), &operatorv1.GetTaskActivityRequest{TaskId: "x"})
	if status.Code(err) != codes.Unimplemented {
		t.Errorf("without an activity service: %v", err)
	}
	plugged := newFixture(t, "completed", WithActivity(pluggedActivity{}))
	_, err = operatorv1.NewActivityServiceClient(plugged.authed).GetTaskActivity(context.Background(), &operatorv1.GetTaskActivityRequest{TaskId: "x"})
	if status.Code(err) != codes.AlreadyExists {
		t.Errorf("with one plugged in: %v", err)
	}
}

type pluggedActivity struct {
	operatorv1.UnimplementedActivityServiceServer
}

func (pluggedActivity) GetTaskActivity(context.Context, *operatorv1.GetTaskActivityRequest) (*operatorv1.GetTaskActivityResponse, error) {
	return nil, status.Error(codes.AlreadyExists, "the plugged-in service answered")
}

func TestStatusErrorNamesTheKindAndTheCode(t *testing.T) {
	for _, c := range []struct {
		err        error
		kind, code string
	}{
		{domain.NotFoundError{Kind: "task", ID: "t-1"}, "not_found", "NotFound"},
		{domain.ValidationError{Field: "f", Reason: "r"}, "validation", "InvalidArgument"},
		{domain.StateError{Subject: "s", Current: "c", Attempted: "a"}, "state", "FailedPrecondition"},
		{domain.ApprovalExpiredError{}, "approval_expired", "FailedPrecondition"},
		{domain.ConflictError{}, "conflict", "Aborted"},
		{domain.ForbiddenError{Reason: "no"}, "forbidden", "PermissionDenied"},
		{unixapi.UnauthorizedError{}, "unauthorized", "Unauthenticated"},
		{errors.New("disk on fire"), "internal", "Internal"},
		{fmt.Errorf("wrapped: %w", domain.NotFoundError{Kind: "bot", ID: "b"}), "not_found", "NotFound"},
	} {
		if k, code := kind(t, statusError(c.err)); k != c.kind || code != c.code {
			t.Errorf("%T: %s %s, want %s %s", c.err, k, code, c.kind, c.code)
		}
	}
	revoked := errorDetail(t, statusError(domain.ForbiddenError{Reason: "revoked", Revoked: true}))
	if !revoked.GetRevoked() {
		t.Errorf("revoked: %v", revoked)
	}
	if statusError(nil) != nil {
		t.Error("nil became an error")
	}
	if got := statusError(context.Canceled); status.Code(got) != codes.Canceled {
		t.Errorf("canceled: %v", got)
	}
	if got := statusError(context.DeadlineExceeded); status.Code(got) != codes.DeadlineExceeded {
		t.Errorf("deadline: %v", got)
	}
	already := status.Error(codes.AlreadyExists, "x")
	if statusError(already) != already {
		t.Error("a status was rewritten")
	}
}

func TestActorComesFromTheMetadata(t *testing.T) {
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs(ActorMetadata, "alice", ActorMetadata, "bob"))
	acting, err := actorFrom(ctx)
	if err != nil || actorOf(acting) != "alice" {
		t.Error("the first value is the actor")
	}
	if none, err := actorFrom(context.Background()); err != nil || actorOf(none) != "" {
		t.Error("no metadata is no actor")
	}
	var seen string
	handler := func(ctx context.Context, _ any) (any, error) { seen = actorOf(ctx); return nil, nil }
	if _, err := (actorInterceptor{}).unary(ctx, nil, &grpc.UnaryServerInfo{}, handler); err != nil || seen != "alice" {
		t.Errorf("unary: %q %v", seen, err)
	}
}

func TestAPanickingHandlerAnswersInternal(t *testing.T) {
	_, err := (errorInterceptor{}).unary(context.Background(), nil, &grpc.UnaryServerInfo{}, func(context.Context, any) (any, error) { panic("boom") })
	if k, c := kind(t, err); k != "internal" || c != "Internal" {
		t.Errorf("unary: %s %s", k, c)
	}
	err = (errorInterceptor{}).stream(nil, nil, &grpc.StreamServerInfo{}, func(any, grpc.ServerStream) error { panic("boom") })
	if k, c := kind(t, err); k != "internal" || c != "Internal" {
		t.Errorf("stream: %s %s", k, c)
	}
}

func TestActorBehindACertificateIsTheOperator(t *testing.T) {
	base := withOperator(context.Background(), "alice")
	acting, err := actorFrom(base)
	if err != nil || actorOf(acting) != "alice" {
		t.Errorf("no key: %q %v", actorOf(acting), err)
	}
	same := metadata.NewIncomingContext(base, metadata.Pairs(ActorMetadata, "alice"))
	if acting, err := actorFrom(same); err != nil || actorOf(acting) != "alice" {
		t.Errorf("the same name: %q %v", actorOf(acting), err)
	}
	other := metadata.NewIncomingContext(base, metadata.Pairs(ActorMetadata, "alice", ActorMetadata, "mallory"))
	if _, err := actorFrom(other); err == nil || !strings.Contains(err.Error(), "mallory") {
		t.Errorf("another name: %v", err)
	}
	handler := func(context.Context, any) (any, error) { t.Error("the handler ran"); return nil, nil }
	if _, err := (actorInterceptor{}).unary(other, nil, &grpc.UnaryServerInfo{}, handler); err == nil {
		t.Error("unary served another actor")
	}
	if err := (actorInterceptor{}).stream(nil, wrappedStream{ctx: other}, &grpc.StreamServerInfo{}, func(any, grpc.ServerStream) error { t.Error("the handler ran"); return nil }); err == nil {
		t.Error("stream served another actor")
	}
	var seen string
	if err := (actorInterceptor{}).stream(nil, wrappedStream{ctx: base}, &grpc.StreamServerInfo{}, func(_ any, s grpc.ServerStream) error { seen = actorOf(s.Context()); return nil }); err != nil || seen != "alice" {
		t.Errorf("stream: %q %v", seen, err)
	}
}
