package grpcclient

import (
	"context"

	"google.golang.org/grpc/metadata"

	"github.com/underpass-ai/axlr-bots/adapters/unixapi"
	operatorv1 "github.com/underpass-ai/axlr-bots/api/gen/axlrbots/operator/v1"
	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

// ActorKey is the metadata key that says who decides a pending call.
const ActorKey = "x-axlr-actor"

func (c *Client) SubmitTask(ctx context.Context, request unixapi.SubmitRequest) (unixapi.SubmitResponse, error) {
	answer, err := c.tasks.SubmitTask(ctx, &operatorv1.SubmitTaskRequest{
		BotId: request.BotID, Objective: request.Objective, IdempotencyKey: request.IdempotencyKey,
		CorrelationId: request.CorrelationID, Priority: request.Priority,
	})
	if err != nil {
		return unixapi.SubmitResponse{}, fromStatus(c.address, err)
	}
	return unixapi.SubmitResponse{Created: answer.GetCreated(), Task: taskSnapshot(answer.GetTask())}, nil
}

func (c *Client) RunTask(ctx context.Context, id string) (unixapi.RunResponse, error) {
	answer, err := c.tasks.RunTask(ctx, &operatorv1.RunTaskRequest{TaskId: id})
	if err != nil {
		return unixapi.RunResponse{}, fromStatus(c.address, err)
	}
	return unixapi.RunResponse{Task: taskSnapshot(answer.GetTask()), Execution: executionSnapshot(answer.GetExecution())}, nil
}

// DecideTask decides the pending call as request.DecidedBy: it goes in
// the decided_by field and in the actor metadata, which the contract
// compares, so the two always agree.
func (c *Client) DecideTask(ctx context.Context, id string, request unixapi.DecideRequest) (unixapi.RunResponse, error) {
	if request.DecidedBy != "" {
		ctx = metadata.AppendToOutgoingContext(ctx, ActorKey, request.DecidedBy)
	}
	answer, err := c.tasks.DecideTask(ctx, &operatorv1.DecideTaskRequest{TaskId: id, Decision: request.Decision, CallId: request.CallID, DecidedBy: request.DecidedBy})
	if err != nil {
		return unixapi.RunResponse{}, fromStatus(c.address, err)
	}
	return unixapi.RunResponse{Task: taskSnapshot(answer.GetTask()), Execution: executionSnapshot(answer.GetExecution())}, nil
}

func (c *Client) CancelTask(ctx context.Context, id string) (unixapi.CancelResponse, error) {
	answer, err := c.tasks.CancelTask(ctx, &operatorv1.CancelTaskRequest{TaskId: id})
	if err != nil {
		return unixapi.CancelResponse{}, fromStatus(c.address, err)
	}
	return unixapi.CancelResponse{Task: taskSnapshot(answer.GetTask()), Execution: executionPtr(answer.GetExecution()), Changed: answer.GetChanged()}, nil
}

func (c *Client) SendTaskMessage(ctx context.Context, id string, request unixapi.MessageRequest) (application.SentTaskMessage, error) {
	answer, err := c.tasks.SendTaskMessage(ctx, &operatorv1.SendTaskMessageRequest{TaskId: id, Text: request.Text})
	if err != nil {
		return application.SentTaskMessage{}, fromStatus(c.address, err)
	}
	return application.SentTaskMessage{TaskID: domain.TaskID(answer.GetTaskId()), ExecutionID: domain.ExecutionID(answer.GetExecutionId()), CreatedAt: timeOf(answer.GetCreatedAt())}, nil
}

func (c *Client) GetTask(ctx context.Context, id string) (domain.TaskSnapshot, error) {
	answer, err := c.tasks.GetTask(ctx, &operatorv1.GetTaskRequest{TaskId: id})
	if err != nil {
		return domain.TaskSnapshot{}, fromStatus(c.address, err)
	}
	return taskSnapshot(answer.GetTask()), nil
}

func (c *Client) ListTasks(ctx context.Context, query application.TaskQuery) ([]domain.TaskSnapshot, error) {
	answer, err := c.tasks.ListTasks(ctx, &operatorv1.ListTasksRequest{BotId: query.BotID, Status: query.Status, Since: stampOf(query.Since), Limit: int32(query.Limit)})
	if err != nil {
		return nil, fromStatus(c.address, err)
	}
	return taskSnapshots(answer.GetTasks()), nil
}

func (c *Client) ListTaskEvents(ctx context.Context, query application.TaskEventsQuery) ([]domain.Event, error) {
	answer, err := c.tasks.ListTaskEvents(ctx, &operatorv1.ListTaskEventsRequest{TaskId: query.ID.String(), After: query.After, Limit: int32(query.Limit)})
	if err != nil {
		return nil, fromStatus(c.address, err)
	}
	return eventsOf(answer.GetEvents()), nil
}

func (c *Client) ListExecutions(ctx context.Context, id string) ([]domain.ExecutionSnapshot, error) {
	answer, err := c.tasks.ListTaskExecutions(ctx, &operatorv1.ListTaskExecutionsRequest{TaskId: id})
	if err != nil {
		return nil, fromStatus(c.address, err)
	}
	return executionSnapshots(answer.GetExecutions()), nil
}

func (c *Client) GetReport(ctx context.Context, id string) (unixapi.ReportResponse, error) {
	answer, err := c.tasks.GetTaskReport(ctx, &operatorv1.GetTaskReportRequest{TaskId: id})
	if err != nil {
		return unixapi.ReportResponse{}, fromStatus(c.address, err)
	}
	return unixapi.ReportResponse{TaskID: answer.GetTaskId(), Reference: answer.GetReference(), Content: answer.GetContent()}, nil
}

// FireTrigger fires a webhook trigger. Triggers are HTTP routes, not
// RPCs of the operator API: the client that was given a firer uses it,
// and any other says so.
func (c *Client) FireTrigger(ctx context.Context, request unixapi.FireRequest) (unixapi.FireResponse, error) {
	if c.triggers == nil {
		return unixapi.FireResponse{}, domain.ValidationError{Field: "trigger", Reason: "firing a webhook trigger is not part of the gRPC operator API; POST the event to the trigger's webhook route (/v1/triggers/<bot>/<trigger>)"}
	}
	return c.triggers.FireTrigger(ctx, request)
}
