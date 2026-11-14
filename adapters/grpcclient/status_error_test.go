package grpcclient

import (
	"errors"
	"github.com/underpass-ai/axlr-bots/application"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	operatorv1 "github.com/underpass-ai/axlr-bots/api/gen/axlrbots/operator/v1"
	"github.com/underpass-ai/axlr-bots/domain"
)

// detailed is a status with an ErrorDetail, as the control plane sends.
func detailed(t *testing.T, code codes.Code, detail *operatorv1.ErrorDetail) error {
	t.Helper()
	withDetails, err := status.New(code, detail.GetMessage()).WithDetails(detail)
	if err != nil {
		t.Fatal(err)
	}
	return withDetails.Err()
}

func TestErrorsComeBackAsTheTypesTheCommandKnows(t *testing.T) {
	h := newHarness(t)
	call := func() error { _, err := h.client.GetTask(t.Context(), "t-9"); return err }

	h.fake.fail(method("TasksService", "GetTask"), detailed(t, codes.NotFound, &operatorv1.ErrorDetail{Kind: "not_found", Message: "task t-9 not found", Resource: "task", Id: "t-9"}))
	var missing domain.NotFoundError
	if err := call(); !errors.As(err, &missing) || missing.Kind != "task" || missing.ID != "t-9" || err.Error() != "task t-9 not found" {
		t.Errorf("not found: %#v", err)
	}
	h.fake.fail(method("TasksService", "GetTask"), detailed(t, codes.NotFound, &operatorv1.ErrorDetail{Kind: "not_found", Message: "m"}))
	if err := call(); !errors.As(err, &missing) || missing.Kind != "resource" {
		t.Errorf("not found without a resource: %#v", err)
	}

	h.fake.fail(method("TasksService", "GetTask"), detailed(t, codes.PermissionDenied, &operatorv1.ErrorDetail{Kind: "forbidden", Message: "forbidden: node revoked", Revoked: true}))
	var forbidden domain.ForbiddenError
	if err := call(); !errors.As(err, &forbidden) || forbidden.Reason != "node revoked" || !forbidden.Revoked || !domain.IsRevoked(err) {
		t.Errorf("forbidden: %#v", err)
	}

	h.fake.fail(method("TasksService", "GetTask"), detailed(t, codes.Unauthenticated, &operatorv1.ErrorDetail{Kind: "unauthorized", Message: "no"}))
	if err := call(); !errors.As(err, new(application.UnauthorizedError)) {
		t.Errorf("unauthorized: %#v", err)
	}

	for kind, code := range map[string]codes.Code{"validation": codes.InvalidArgument, "state": codes.FailedPrecondition, "approval_expired": codes.FailedPrecondition, "conflict": codes.Aborted, "too_large": codes.ResourceExhausted, "internal": codes.Internal} {
		h.fake.fail(method("TasksService", "GetTask"), detailed(t, code, &operatorv1.ErrorDetail{Kind: kind, Message: "the daemon's words"}))
		var remote application.RemoteError
		if err := call(); !errors.As(err, &remote) || remote.Kind != kind || err.Error() != "the daemon's words" {
			t.Errorf("%s: %#v", kind, err)
		}
	}
	h.fake.fail(method("TasksService", "GetTask"), detailed(t, codes.Internal, &operatorv1.ErrorDetail{Kind: "state"}))
	if err := call(); err == nil || err.Error() == "" {
		t.Errorf("a detail with no message still says something: %v", err)
	}
}

func TestAStatusWithoutDetailIsReadByItsCode(t *testing.T) {
	h := newHarness(t)
	call := func() error { _, err := h.client.GetTask(t.Context(), "t"); return err }
	cases := map[codes.Code]string{
		codes.NotFound: "not_found", codes.InvalidArgument: "validation", codes.FailedPrecondition: "state", codes.Aborted: "conflict",
		codes.PermissionDenied: "forbidden", codes.Unauthenticated: "unauthorized", codes.ResourceExhausted: "too_large", codes.Internal: "internal",
	}
	for code, kind := range cases {
		h.fake.fail(method("TasksService", "GetTask"), status.Error(code, "plain"))
		err := call()
		var remote application.RemoteError
		switch kind {
		case "not_found":
			if !domain.IsNotFound(err) {
				t.Errorf("%v: %#v", code, err)
			}
		case "forbidden":
			if !domain.IsForbidden(err) {
				t.Errorf("%v: %#v", code, err)
			}
		case "unauthorized":
			if !errors.As(err, new(application.UnauthorizedError)) {
				t.Errorf("%v: %#v", code, err)
			}
		default:
			if !errors.As(err, &remote) || remote.Kind != kind {
				t.Errorf("%v: %#v", code, err)
			}
		}
	}
	h.fake.fail(method("TasksService", "GetTask"), status.Error(codes.Unavailable, "connection refused"))
	if err := call(); err == nil || err.Error() != "control plane at bufnet: connection refused" {
		t.Errorf("unavailable names the address: %v", err)
	}
	if err := fromStatus("x", errors.New("plain")); err.Error() != "control plane at x: plain" {
		t.Errorf("not a status: %v", err)
	}
	if fromStatus("x", nil) != nil {
		t.Error("no error is no error")
	}
	already := domain.NotFoundError{Kind: "bot", ID: "b"}
	if got := fromStatus("x", already); !errors.Is(got, already) {
		t.Errorf("an error already rebuilt passes through: %v", got)
	}
}

func TestEveryCallMapsItsError(t *testing.T) {
	h := newHarness(t)
	boom := detailed(t, codes.FailedPrecondition, &operatorv1.ErrorDetail{Kind: "state", Message: "refused"})
	for _, name := range []string{
		"BotsService/RegisterBot", "BotsService/EnableBot", "BotsService/PauseBot", "BotsService/StopBot", "BotsService/DeleteBot", "BotsService/UpdateBot", "BotsService/StageBot",
		"BotsService/GetStagedBot", "BotsService/ApplyStagedBot", "BotsService/UnstageBot", "BotsService/ListBots", "BotsService/GetBot", "BotsService/ListBotEvents",
		"GroupsService/ListGroups", "GroupsService/GetGroup", "GroupsService/ListGroupTopics", "GroupsService/ListGroupEvents", "GroupsService/PublishGroupEvent",
		"TasksService/SubmitTask", "TasksService/RunTask", "TasksService/DecideTask", "TasksService/CancelTask", "TasksService/SendTaskMessage", "TasksService/GetTask",
		"TasksService/ListTasks", "TasksService/ListTaskEvents", "TasksService/ListTaskExecutions", "TasksService/GetTaskReport", "OverviewService/GetOverview",
		"ExecutionsService/GetExecution", "ExecutionsService/ListTranscripts", "NodesService/ListNodes", "NodesService/GetNode", "NodesService/CreateEnrollment", "NodesService/RevokeNode",
		"ActivityService/GetTaskActivity",
	} {
		h.fake.fail("/axlrbots.operator.v1."+name, boom)
	}
	h.fake.fail("GetArtifact", boom)
	h.fake.fail("GetTranscript", boom)
	h.fake.fail("WatchTaskActivity", boom)
	ctx := t.Context()
	calls := map[string]func() error{
		"RegisterBot":       func() error { _, err := h.client.RegisterBot(ctx, application0); return err },
		"EnableBot":         func() error { _, err := h.client.EnableBot(ctx, "b"); return err },
		"PauseBot":          func() error { _, err := h.client.PauseBot(ctx, "b"); return err },
		"StopBot":           func() error { _, err := h.client.StopBot(ctx, "b"); return err },
		"DeleteBot":         func() error { _, err := h.client.DeleteBot(ctx, "b"); return err },
		"UpdateBot":         func() error { _, err := h.client.UpdateBot(ctx, "b", patch0); return err },
		"StageBot":          func() error { _, err := h.client.StageBot(ctx, "b", patch0); return err },
		"GetStagedBot":      func() error { _, err := h.client.GetStagedBot(ctx, "b"); return err },
		"ApplyStagedBot":    func() error { _, err := h.client.ApplyStagedBot(ctx, "b"); return err },
		"UnstageBot":        func() error { _, err := h.client.UnstageBot(ctx, "b"); return err },
		"ListBots":          func() error { _, err := h.client.ListBots(ctx); return err },
		"GetBot":            func() error { _, err := h.client.GetBot(ctx, "b"); return err },
		"ListEvents":        func() error { _, err := h.client.ListEvents(ctx, "b", 1); return err },
		"ListGroups":        func() error { _, err := h.client.ListGroups(ctx); return err },
		"GetGroup":          func() error { _, err := h.client.GetGroup(ctx, "g", ""); return err },
		"ListGroupTopics":   func() error { _, err := h.client.ListGroupTopics(ctx, "g", ""); return err },
		"ListGroupEvents":   func() error { _, err := h.client.ListGroupEvents(ctx, query0); return err },
		"PublishGroupEvent": func() error { _, err := h.client.PublishGroupEvent(ctx, publish0); return err },
		"SubmitTask":        func() error { _, err := h.client.SubmitTask(ctx, application.SubmitRequest{}); return err },
		"RunTask":           func() error { _, err := h.client.RunTask(ctx, "t"); return err },
		"DecideTask":        func() error { _, err := h.client.DecideTask(ctx, "t", application.DecideRequest{}); return err },
		"CancelTask":        func() error { _, err := h.client.CancelTask(ctx, "t"); return err },
		"SendTaskMessage":   func() error { _, err := h.client.SendTaskMessage(ctx, "t", application.MessageRequest{}); return err },
		"GetTask":           func() error { _, err := h.client.GetTask(ctx, "t"); return err },
		"ListTasks":         func() error { _, err := h.client.ListTasks(ctx, taskQuery0); return err },
		"ListTaskEvents":    func() error { _, err := h.client.ListTaskEvents(ctx, taskEvents0); return err },
		"ListExecutions":    func() error { _, err := h.client.ListExecutions(ctx, "t"); return err },
		"GetReport":         func() error { _, err := h.client.GetReport(ctx, "t"); return err },
		"GetOverview":       func() error { _, err := h.client.GetOverview(ctx, t0); return err },
		"GetExecution":      func() error { _, err := h.client.GetExecution(ctx, "e"); return err },
		"GetArtifact":       func() error { _, err := h.client.GetArtifact(ctx, "e", "a"); return err },
		"ListTranscripts":   func() error { _, err := h.client.ListTranscripts(ctx, 0, 0); return err },
		"GetTranscript":     func() error { _, err := h.client.GetTranscript(ctx, "e", "s"); return err },
		"ListNodes":         func() error { _, err := h.client.ListNodes(ctx); return err },
		"GetNode":           func() error { _, err := h.client.GetNode(ctx, "n"); return err },
		"CreateEnrollment": func() error {
			_, err := h.client.CreateEnrollmentToken(ctx, application.EnrollTokenRequest{})
			return err
		},
		"RevokeNode":    func() error { _, err := h.client.RevokeNode(ctx, "n"); return err },
		"ActivityGet":   func() error { _, err := h.client.Get(ctx, "t", options0); return err },
		"ActivityWatch": func() error { return h.client.Watch(ctx, "t", options0, "", nil) },
	}
	for name, call := range calls {
		var remote application.RemoteError
		if err := call(); !errors.As(err, &remote) || remote.Kind != "state" || remote.Message != "refused" {
			t.Errorf("%s: %#v", name, err)
		}
	}
}
