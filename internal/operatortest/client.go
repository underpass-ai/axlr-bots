// Package operatortest is test support: one value that speaks the node
// protocol over the daemon's HTTP socket and the operator API in the way
// the test has at hand (the service in process, or a gRPC client), so a
// test that drives a node and an operator needs one handle.
package operatortest

import (
	"context"

	"github.com/underpass-ai/axlr-bots/adapters/unixapi"
	"github.com/underpass-ai/axlr-bots/application"
)

// Client is the node protocol's client and the operator service together.
type Client struct {
	*unixapi.Client
	application.OperatorService
}

// New joins the node client and the operator service.
func New(node *unixapi.Client, operator application.OperatorService) *Client {
	return &Client{Client: node, OperatorService: operator}
}

// FireTrigger is the operator's: through the service in hand.
func (c *Client) FireTrigger(ctx context.Context, request application.FireRequest) (application.FireResponse, error) {
	return c.OperatorService.FireTrigger(ctx, request)
}
