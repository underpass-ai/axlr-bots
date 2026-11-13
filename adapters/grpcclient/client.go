// Package grpcclient is the operator's client of the control plane over
// gRPC: the same Service the command used over HTTP (unixapi.Service),
// spoken through the services of axlrbots.operator.v1. It dials the
// daemon's local socket (no credentials: the file is the permission) or
// an address over TLS with the fleet CA and the operator token in the
// `authorization` metadata key. Mappers turn the wire messages into
// the types of the application and back, in their own files.
package grpcclient

import (
	"io"

	"google.golang.org/grpc"

	"github.com/underpass-ai/axlr-bots/adapters/unixapi"
	operatorv1 "github.com/underpass-ai/axlr-bots/api/gen/axlrbots/operator/v1"
)

var _ unixapi.Service = (*Client)(nil)

// Client implements unixapi.Service over the operator API.
type Client struct {
	address  string
	closer   io.Closer
	bots     operatorv1.BotsServiceClient
	groups   operatorv1.GroupsServiceClient
	tasks    operatorv1.TasksServiceClient
	execs    operatorv1.ExecutionsServiceClient
	nodes    operatorv1.NodesServiceClient
	overview operatorv1.OverviewServiceClient
	activity operatorv1.ActivityServiceClient
	triggers TriggerFirer
}

// New is a client over the connection; the token, when there is one,
// goes in every call. address names the control plane in the errors. The
// caller closes the connection, or Close does when the client dialed it.
func New(conn grpc.ClientConnInterface, address, token string) *Client {
	authed := authedConn{conn: conn, token: token}
	return &Client{
		address: address, bots: operatorv1.NewBotsServiceClient(authed), groups: operatorv1.NewGroupsServiceClient(authed),
		tasks: operatorv1.NewTasksServiceClient(authed), execs: operatorv1.NewExecutionsServiceClient(authed),
		nodes: operatorv1.NewNodesServiceClient(authed), overview: operatorv1.NewOverviewServiceClient(authed),
		activity: operatorv1.NewActivityServiceClient(authed),
	}
}

// WithTriggerFirer returns the client that fires webhook triggers
// through the firer: triggers are HTTP routes, not part of the operator
// API.
func (c *Client) WithTriggerFirer(firer TriggerFirer) *Client {
	copied := *c
	copied.triggers = firer
	return &copied
}

// Close closes the connection the client dialed.
func (c *Client) Close() error {
	if c.closer == nil {
		return nil
	}
	return c.closer.Close()
}
