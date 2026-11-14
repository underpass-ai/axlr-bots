package unixapi

import (
	"context"
	"net/http"
	"net/url"

	"github.com/underpass-ai/axlr-bots/application"
)

// FireTrigger posts the event to the trigger's webhook route: what the
// command does over the daemon's socket, where no secret is needed.
func (c *Client) FireTrigger(ctx context.Context, request application.FireRequest) (application.FireResponse, error) {
	headers := http.Header{}
	if request.DeliveryID != "" {
		headers.Set(IdempotencyKeyHeader, request.DeliveryID)
	}
	var answer firedAnswer
	err := c.callWith(ctx, http.MethodPost, "/v1/triggers/"+url.PathEscape(request.BotID)+"/"+url.PathEscape(request.Trigger), headers, request.Body, &answer)
	return answer.response(), err
}
