package grpcapi

import (
	"context"

	"github.com/underpass-ai/axlr-bots/adapters/unixapi"
	operatorv1 "github.com/underpass-ai/axlr-bots/api/gen/axlrbots/operator/v1"
	"github.com/underpass-ai/axlr-bots/domain"
)

// botsService is BotsService over the same service the HTTP routes use.
type botsService struct {
	operatorv1.UnimplementedBotsServiceServer
	local *unixapi.Local
}

func (s botsService) ListBots(ctx context.Context, _ *operatorv1.ListBotsRequest) (*operatorv1.ListBotsResponse, error) {
	bots, err := s.local.ListBots(ctx)
	if err != nil {
		return nil, err
	}
	return &operatorv1.ListBotsResponse{Bots: botsToProto(bots)}, nil
}

func (s botsService) GetBot(ctx context.Context, request *operatorv1.GetBotRequest) (*operatorv1.GetBotResponse, error) {
	bot, err := s.local.GetBot(ctx, request.GetBotId())
	if err != nil {
		return nil, err
	}
	return &operatorv1.GetBotResponse{Bot: botToProto(bot)}, nil
}

func (s botsService) RegisterBot(ctx context.Context, request *operatorv1.RegisterBotRequest) (*operatorv1.RegisterBotResponse, error) {
	bot, err := s.local.RegisterBot(ctx, definitionFromProto(request.GetDefinition()))
	if err != nil {
		return nil, err
	}
	return &operatorv1.RegisterBotResponse{Bot: botToProto(bot)}, nil
}

func (s botsService) UpdateBot(ctx context.Context, request *operatorv1.UpdateBotRequest) (*operatorv1.UpdateBotResponse, error) {
	bot, err := s.local.UpdateBot(ctx, request.GetBotId(), patchFromProto(request.GetPatch()))
	if err != nil {
		return nil, err
	}
	return &operatorv1.UpdateBotResponse{Bot: botToProto(bot)}, nil
}

func (s botsService) DeleteBot(ctx context.Context, request *operatorv1.DeleteBotRequest) (*operatorv1.DeleteBotResponse, error) {
	deleted, err := s.local.DeleteBot(ctx, request.GetBotId())
	if err != nil {
		return nil, err
	}
	response := &operatorv1.DeleteBotResponse{
		Bot: botToProto(deleted.Bot), Cancelled: deleted.Cancelled, ChildrenCancelled: deleted.ChildrenCancelled,
		TimersRemoved: int32(deleted.TimersRemoved), NotificationsDropped: int32(deleted.NotificationsDropped),
	}
	for _, r := range deleted.NamedBy {
		response.NamedBy = append(response.NamedBy, &operatorv1.BotReference{BotId: r.BotID, How: r.How, Topic: r.Topic})
	}
	return response, nil
}

func (s botsService) EnableBot(ctx context.Context, request *operatorv1.EnableBotRequest) (*operatorv1.EnableBotResponse, error) {
	bot, err := s.local.EnableBot(ctx, request.GetBotId())
	if err != nil {
		return nil, err
	}
	return &operatorv1.EnableBotResponse{Bot: botToProto(bot)}, nil
}

func (s botsService) PauseBot(ctx context.Context, request *operatorv1.PauseBotRequest) (*operatorv1.PauseBotResponse, error) {
	bot, err := s.local.PauseBot(ctx, request.GetBotId())
	if err != nil {
		return nil, err
	}
	return &operatorv1.PauseBotResponse{Bot: botToProto(bot)}, nil
}

func (s botsService) StopBot(ctx context.Context, request *operatorv1.StopBotRequest) (*operatorv1.StopBotResponse, error) {
	stopped, err := s.local.StopBot(ctx, request.GetBotId())
	if err != nil {
		return nil, err
	}
	response := &operatorv1.StopBotResponse{Bot: botToProto(stopped.Bot), Cancelled: stopped.Cancelled}
	for _, r := range stopped.NotCancelled {
		response.NotCancelled = append(response.NotCancelled, &operatorv1.StopRefusal{TaskId: r.TaskID, Reason: r.Reason})
	}
	return response, nil
}

func (s botsService) GetStagedBot(ctx context.Context, request *operatorv1.GetStagedBotRequest) (*operatorv1.GetStagedBotResponse, error) {
	view, err := s.local.GetStagedBot(ctx, request.GetBotId())
	if err != nil {
		return nil, err
	}
	return &operatorv1.GetStagedBotResponse{Version: view.Version, StagedAt: timestamp(view.StagedAt), Patch: patchToProto(view.Patch)}, nil
}

func (s botsService) StageBot(ctx context.Context, request *operatorv1.StageBotRequest) (*operatorv1.StageBotResponse, error) {
	bot, err := s.local.StageBot(ctx, request.GetBotId(), patchFromProto(request.GetPatch()))
	if err != nil {
		return nil, err
	}
	return &operatorv1.StageBotResponse{Bot: botToProto(bot)}, nil
}

func (s botsService) UnstageBot(ctx context.Context, request *operatorv1.UnstageBotRequest) (*operatorv1.UnstageBotResponse, error) {
	bot, err := s.local.UnstageBot(ctx, request.GetBotId())
	if err != nil {
		return nil, err
	}
	return &operatorv1.UnstageBotResponse{Bot: botToProto(bot)}, nil
}

func (s botsService) ApplyStagedBot(ctx context.Context, request *operatorv1.ApplyStagedBotRequest) (*operatorv1.ApplyStagedBotResponse, error) {
	bot, err := s.local.ApplyStagedBot(ctx, request.GetBotId())
	if err != nil {
		return nil, err
	}
	return &operatorv1.ApplyStagedBotResponse{Bot: botToProto(bot)}, nil
}

func (s botsService) ListBotEvents(ctx context.Context, request *operatorv1.ListBotEventsRequest) (*operatorv1.ListBotEventsResponse, error) {
	limit, err := limitOf("limit", request.GetLimit())
	if err != nil {
		return nil, err
	}
	events, err := s.local.ListEvents(ctx, request.GetBotId(), limit)
	if err != nil {
		return nil, err
	}
	return &operatorv1.ListBotEventsResponse{Events: eventsListToProto(events)}, nil
}

func (s botsService) WatchBotEvents(request *operatorv1.WatchBotEventsRequest, stream operatorv1.BotsService_WatchBotEventsServer) error {
	replay, err := limitOf("replay_limit", request.GetReplayLimit())
	if err != nil {
		return err
	}
	ctx := stream.Context()
	read := func(limit int) ([]domain.Event, error) { return s.local.ListEvents(ctx, request.GetBotId(), limit) }
	send := func(e domain.Event) error {
		return stream.Send(&operatorv1.WatchBotEventsResponse{Event: eventToProto(e)})
	}
	return eventWatch{changed: s.local.Changed}.newest(ctx, replay, read, send)
}

func (s botsService) SubmitBotTask(ctx context.Context, request *operatorv1.SubmitBotTaskRequest) (*operatorv1.SubmitBotTaskResponse, error) {
	submitted, err := s.local.SubmitTask(ctx, unixapi.SubmitRequest{
		BotID: request.GetBotId(), Objective: request.GetObjective(), IdempotencyKey: request.GetIdempotencyKey(),
		CorrelationID: request.GetCorrelationId(), Priority: request.GetPriority(),
	})
	if err != nil {
		return nil, err
	}
	return &operatorv1.SubmitBotTaskResponse{Created: submitted.Created, Task: taskToProto(submitted.Task)}, nil
}
