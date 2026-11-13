package grpcclient

import (
	"context"
	"time"

	operatorv1 "github.com/underpass-ai/axlr-bots/api/gen/axlrbots/operator/v1"
	"github.com/underpass-ai/axlr-bots/application"
)

func (c *Client) GetOverview(ctx context.Context, since time.Time) (application.Overview, error) {
	answer, err := c.overview.GetOverview(ctx, &operatorv1.GetOverviewRequest{Since: stampOf(since)})
	if err != nil {
		return application.Overview{}, fromStatus(c.address, err)
	}
	return overviewOf(answer), nil
}
