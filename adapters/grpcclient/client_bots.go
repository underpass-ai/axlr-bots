package grpcclient

import (
	"context"

	operatorv1 "github.com/underpass-ai/axlr-bots/api/gen/axlrbots/operator/v1"
	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

func (c *Client) RegisterBot(ctx context.Context, definition application.BotDefinition) (domain.BotSnapshot, error) {
	message, err := botDefinitionMessage(definition)
	if err != nil {
		return domain.BotSnapshot{}, err
	}
	answer, err := c.bots.RegisterBot(ctx, &operatorv1.RegisterBotRequest{Definition: message})
	if err != nil {
		return domain.BotSnapshot{}, fromStatus(c.address, err)
	}
	return botSnapshot(answer.GetBot()), nil
}

func (c *Client) EnableBot(ctx context.Context, id string) (domain.BotSnapshot, error) {
	answer, err := c.bots.EnableBot(ctx, &operatorv1.EnableBotRequest{BotId: id})
	if err != nil {
		return domain.BotSnapshot{}, fromStatus(c.address, err)
	}
	return botSnapshot(answer.GetBot()), nil
}

func (c *Client) PauseBot(ctx context.Context, id string) (domain.BotSnapshot, error) {
	answer, err := c.bots.PauseBot(ctx, &operatorv1.PauseBotRequest{BotId: id})
	if err != nil {
		return domain.BotSnapshot{}, fromStatus(c.address, err)
	}
	return botSnapshot(answer.GetBot()), nil
}

func (c *Client) StopBot(ctx context.Context, id string) (application.StopResponse, error) {
	answer, err := c.bots.StopBot(ctx, &operatorv1.StopBotRequest{BotId: id})
	if err != nil {
		return application.StopResponse{}, fromStatus(c.address, err)
	}
	response := application.StopResponse{Bot: botSnapshot(answer.GetBot()), Cancelled: nonNil(answer.GetCancelled()), NotCancelled: []application.StopRefusal{}}
	for _, refusal := range answer.GetNotCancelled() {
		response.NotCancelled = append(response.NotCancelled, application.StopRefusal{TaskID: refusal.GetTaskId(), Reason: refusal.GetReason()})
	}
	return response, nil
}

func (c *Client) DeleteBot(ctx context.Context, id string) (application.DeleteResponse, error) {
	answer, err := c.bots.DeleteBot(ctx, &operatorv1.DeleteBotRequest{BotId: id})
	if err != nil {
		return application.DeleteResponse{}, fromStatus(c.address, err)
	}
	response := application.DeleteResponse{
		Bot: botSnapshot(answer.GetBot()), Cancelled: nonNil(answer.GetCancelled()), ChildrenCancelled: nonNil(answer.GetChildrenCancelled()),
		TimersRemoved: int(answer.GetTimersRemoved()), NotificationsDropped: int(answer.GetNotificationsDropped()), NamedBy: []application.BotReference{},
	}
	for _, reference := range answer.GetNamedBy() {
		response.NamedBy = append(response.NamedBy, application.BotReference{BotID: reference.GetBotId(), How: reference.GetHow(), Topic: reference.GetTopic()})
	}
	return response, nil
}

func (c *Client) UpdateBot(ctx context.Context, id string, patch application.BotPatch) (domain.BotSnapshot, error) {
	message, err := botPatchMessage(patch)
	if err != nil {
		return domain.BotSnapshot{}, err
	}
	answer, err := c.bots.UpdateBot(ctx, &operatorv1.UpdateBotRequest{BotId: id, Patch: message})
	if err != nil {
		return domain.BotSnapshot{}, fromStatus(c.address, err)
	}
	return botSnapshot(answer.GetBot()), nil
}

func (c *Client) StageBot(ctx context.Context, id string, patch application.BotPatch) (domain.BotSnapshot, error) {
	message, err := botPatchMessage(patch)
	if err != nil {
		return domain.BotSnapshot{}, err
	}
	answer, err := c.bots.StageBot(ctx, &operatorv1.StageBotRequest{BotId: id, Patch: message})
	if err != nil {
		return domain.BotSnapshot{}, fromStatus(c.address, err)
	}
	return botSnapshot(answer.GetBot()), nil
}

func (c *Client) GetStagedBot(ctx context.Context, id string) (application.StagedBotView, error) {
	answer, err := c.bots.GetStagedBot(ctx, &operatorv1.GetStagedBotRequest{BotId: id})
	if err != nil {
		return application.StagedBotView{}, fromStatus(c.address, err)
	}
	return application.StagedBotView{Version: answer.GetVersion(), StagedAt: timeOf(answer.GetStagedAt()), Patch: botPatch(answer.GetPatch())}, nil
}

func (c *Client) ApplyStagedBot(ctx context.Context, id string) (domain.BotSnapshot, error) {
	answer, err := c.bots.ApplyStagedBot(ctx, &operatorv1.ApplyStagedBotRequest{BotId: id})
	if err != nil {
		return domain.BotSnapshot{}, fromStatus(c.address, err)
	}
	return botSnapshot(answer.GetBot()), nil
}

func (c *Client) UnstageBot(ctx context.Context, id string) (domain.BotSnapshot, error) {
	answer, err := c.bots.UnstageBot(ctx, &operatorv1.UnstageBotRequest{BotId: id})
	if err != nil {
		return domain.BotSnapshot{}, fromStatus(c.address, err)
	}
	return botSnapshot(answer.GetBot()), nil
}

func (c *Client) ListBots(ctx context.Context) ([]domain.BotSnapshot, error) {
	answer, err := c.bots.ListBots(ctx, &operatorv1.ListBotsRequest{})
	if err != nil {
		return nil, fromStatus(c.address, err)
	}
	return botSnapshots(answer.GetBots()), nil
}

func (c *Client) GetBot(ctx context.Context, id string) (domain.BotSnapshot, error) {
	answer, err := c.bots.GetBot(ctx, &operatorv1.GetBotRequest{BotId: id})
	if err != nil {
		return domain.BotSnapshot{}, fromStatus(c.address, err)
	}
	return botSnapshot(answer.GetBot()), nil
}

func (c *Client) ListEvents(ctx context.Context, id string, limit int) ([]domain.Event, error) {
	answer, err := c.bots.ListBotEvents(ctx, &operatorv1.ListBotEventsRequest{BotId: id, Limit: int32(limit)})
	if err != nil {
		return nil, fromStatus(c.address, err)
	}
	return eventsOf(answer.GetEvents()), nil
}
