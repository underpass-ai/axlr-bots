package grpcapi

import (
	"context"

	operatorv1 "github.com/underpass-ai/axlr-bots/api/gen/axlrbots/operator/v1"
	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

// tasksService is TasksService over the same service the HTTP routes use.
type tasksService struct {
	operatorv1.UnimplementedTasksServiceServer
	local *application.LocalOperator
}

func (s tasksService) ListTasks(ctx context.Context, request *operatorv1.ListTasksRequest) (*operatorv1.ListTasksResponse, error) {
	limit, err := limitOf("limit", request.GetLimit())
	if err != nil {
		return nil, err
	}
	tasks, err := s.local.ListTasks(ctx, application.TaskQuery{BotID: request.GetBotId(), Status: request.GetStatus(), Since: instant(request.GetSince()), Limit: limit})
	if err != nil {
		return nil, err
	}
	return &operatorv1.ListTasksResponse{Tasks: tasksToProto(tasks)}, nil
}

func (s tasksService) GetTask(ctx context.Context, request *operatorv1.GetTaskRequest) (*operatorv1.GetTaskResponse, error) {
	task, err := s.local.GetTask(ctx, request.GetTaskId())
	if err != nil {
		return nil, err
	}
	return &operatorv1.GetTaskResponse{Task: taskToProto(task)}, nil
}

func (s tasksService) SubmitTask(ctx context.Context, request *operatorv1.SubmitTaskRequest) (*operatorv1.SubmitTaskResponse, error) {
	submitted, err := s.local.SubmitTask(ctx, application.SubmitRequest{
		BotID: request.GetBotId(), Objective: request.GetObjective(), IdempotencyKey: request.GetIdempotencyKey(),
		CorrelationID: request.GetCorrelationId(), Priority: request.GetPriority(),
	})
	if err != nil {
		return nil, err
	}
	return &operatorv1.SubmitTaskResponse{Created: submitted.Created, Task: taskToProto(submitted.Task)}, nil
}

func (s tasksService) RunTask(ctx context.Context, request *operatorv1.RunTaskRequest) (*operatorv1.RunTaskResponse, error) {
	ran, err := s.local.RunTask(ctx, request.GetTaskId())
	if err != nil {
		return nil, err
	}
	return &operatorv1.RunTaskResponse{Task: taskToProto(ran.Task), Execution: executionToProto(ran.Execution)}, nil
}

func (s tasksService) ApproveTask(ctx context.Context, request *operatorv1.ApproveTaskRequest) (*operatorv1.ApproveTaskResponse, error) {
	decided, err := s.local.DecideTask(ctx, request.GetTaskId(), application.DecideRequest{Decision: application.DecisionApprove, CallID: request.GetCallId(), DecidedBy: actorOf(ctx)})
	if err != nil {
		return nil, err
	}
	return &operatorv1.ApproveTaskResponse{Task: taskToProto(decided.Task), Execution: executionToProto(decided.Execution)}, nil
}

func (s tasksService) DenyTask(ctx context.Context, request *operatorv1.DenyTaskRequest) (*operatorv1.DenyTaskResponse, error) {
	decided, err := s.local.DecideTask(ctx, request.GetTaskId(), application.DecideRequest{Decision: application.DecisionDeny, CallID: request.GetCallId(), DecidedBy: actorOf(ctx)})
	if err != nil {
		return nil, err
	}
	return &operatorv1.DenyTaskResponse{Task: taskToProto(decided.Task), Execution: executionToProto(decided.Execution)}, nil
}

// DecideTask takes who decides from decided_by or from the actor
// metadata; the two that differ are refused, as the HTTP route refuses a
// body that names another actor than the header.
func (s tasksService) DecideTask(ctx context.Context, request *operatorv1.DecideTaskRequest) (*operatorv1.DecideTaskResponse, error) {
	by, header := request.GetDecidedBy(), actorOf(ctx)
	if by != "" && header != "" && by != header {
		return nil, domain.ValidationError{Field: "decided_by", Reason: "names another actor than " + ActorMetadata}
	}
	if by == "" {
		by = header
	}
	decided, err := s.local.DecideTask(ctx, request.GetTaskId(), application.DecideRequest{Decision: request.GetDecision(), CallID: request.GetCallId(), DecidedBy: by})
	if err != nil {
		return nil, err
	}
	return &operatorv1.DecideTaskResponse{Task: taskToProto(decided.Task), Execution: executionToProto(decided.Execution)}, nil
}

func (s tasksService) CancelTask(ctx context.Context, request *operatorv1.CancelTaskRequest) (*operatorv1.CancelTaskResponse, error) {
	cancelled, err := s.local.CancelTask(ctx, request.GetTaskId())
	if err != nil {
		return nil, err
	}
	return &operatorv1.CancelTaskResponse{Task: taskToProto(cancelled.Task), Execution: executionOptionalToProto(cancelled.Execution), Changed: cancelled.Changed}, nil
}

func (s tasksService) SendTaskMessage(ctx context.Context, request *operatorv1.SendTaskMessageRequest) (*operatorv1.SendTaskMessageResponse, error) {
	sent, err := s.local.SendTaskMessage(ctx, request.GetTaskId(), application.MessageRequest{Text: request.GetText()})
	if err != nil {
		return nil, err
	}
	return &operatorv1.SendTaskMessageResponse{TaskId: string(sent.TaskID), ExecutionId: string(sent.ExecutionID), CreatedAt: timestamp(sent.CreatedAt)}, nil
}

func (s tasksService) ListTaskEvents(ctx context.Context, request *operatorv1.ListTaskEventsRequest) (*operatorv1.ListTaskEventsResponse, error) {
	limit, err := limitOf("limit", request.GetLimit())
	if err != nil {
		return nil, err
	}
	events, err := s.local.ListTaskEvents(ctx, application.TaskEventsQuery{ID: domain.TaskID(request.GetTaskId()), After: request.GetAfter(), Limit: limit})
	if err != nil {
		return nil, err
	}
	return &operatorv1.ListTaskEventsResponse{Events: eventsListToProto(events)}, nil
}

func (s tasksService) WatchTaskEvents(request *operatorv1.WatchTaskEventsRequest, stream operatorv1.TasksService_WatchTaskEventsServer) error {
	ctx := stream.Context()
	read := func(after string) ([]domain.Event, error) {
		return s.local.ListTaskEvents(ctx, application.TaskEventsQuery{ID: domain.TaskID(request.GetTaskId()), After: after, Limit: application.MaxEventLimit})
	}
	send := func(e domain.Event) error {
		return stream.Send(&operatorv1.WatchTaskEventsResponse{Event: eventToProto(e)})
	}
	return eventWatch{changed: s.local.Changed}.afterCursor(ctx, request.GetAfter(), read, send)
}

func (s tasksService) ListTaskExecutions(ctx context.Context, request *operatorv1.ListTaskExecutionsRequest) (*operatorv1.ListTaskExecutionsResponse, error) {
	executions, err := s.local.ListExecutions(ctx, request.GetTaskId())
	if err != nil {
		return nil, err
	}
	return &operatorv1.ListTaskExecutionsResponse{Executions: executionsToProto(executions)}, nil
}

func (s tasksService) GetTaskReport(ctx context.Context, request *operatorv1.GetTaskReportRequest) (*operatorv1.GetTaskReportResponse, error) {
	report, err := s.local.GetReport(ctx, request.GetTaskId())
	if err != nil {
		return nil, err
	}
	return &operatorv1.GetTaskReportResponse{TaskId: report.TaskID, Reference: report.Reference, Content: report.Content}, nil
}
