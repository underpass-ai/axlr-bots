package unixapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/underpass-ai/underpass-bots/application"
	"github.com/underpass-ai/underpass-bots/domain"
	"github.com/underpass-ai/underpass-bots/nodeproto"
)

// Client is the Service, and the node protocol's transport, over the
// control plane's API: a Unix socket (a path, or unix:///path) or an
// http:// address on the loopback or the network.
type Client struct {
	http    *http.Client
	base    string
	address string
}

// NewClient talks to the control plane at the address. Runs can take as
// long as a task, so the client sets no overall timeout; the caller's
// context bounds a call.
func NewClient(address string) *Client {
	if strings.HasPrefix(address, "http://") || strings.HasPrefix(address, "https://") {
		return &Client{http: &http.Client{}, base: strings.TrimRight(address, "/"), address: address}
	}
	socketPath := strings.TrimPrefix(address, "unix://")
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var dialer net.Dialer
			return dialer.DialContext(ctx, "unix", socketPath)
		},
	}
	return &Client{http: &http.Client{Transport: transport}, base: "http://underpass-bots", address: socketPath}
}

// Reachable reports whether a daemon answers on the socket.
func Reachable(ctx context.Context, socketPath string) bool {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	var health map[string]string
	return NewClient(socketPath).call(ctx, http.MethodGet, "/healthz", nil, &health) == nil
}

func (c *Client) RegisterBot(ctx context.Context, definition application.BotDefinition) (domain.BotSnapshot, error) {
	var bot domain.BotSnapshot
	return bot, c.call(ctx, http.MethodPost, "/v1/bots", definition, &bot)
}

func (c *Client) EnableBot(ctx context.Context, id string) (domain.BotSnapshot, error) {
	var bot domain.BotSnapshot
	return bot, c.call(ctx, http.MethodPost, "/v1/bots/"+url.PathEscape(id)+"/enable", nil, &bot)
}

func (c *Client) ListBots(ctx context.Context) ([]domain.BotSnapshot, error) {
	bots := []domain.BotSnapshot{}
	return bots, c.call(ctx, http.MethodGet, "/v1/bots", nil, &bots)
}

func (c *Client) GetBot(ctx context.Context, id string) (domain.BotSnapshot, error) {
	var bot domain.BotSnapshot
	return bot, c.call(ctx, http.MethodGet, "/v1/bots/"+url.PathEscape(id), nil, &bot)
}

func (c *Client) SubmitTask(ctx context.Context, request SubmitRequest) (SubmitResponse, error) {
	var response SubmitResponse
	return response, c.call(ctx, http.MethodPost, "/v1/tasks", request, &response)
}

func (c *Client) RunTask(ctx context.Context, id string) (RunResponse, error) {
	var response RunResponse
	return response, c.call(ctx, http.MethodPost, "/v1/tasks/"+url.PathEscape(id)+"/run", nil, &response)
}

func (c *Client) DecideTask(ctx context.Context, id, decision string) (RunResponse, error) {
	var response RunResponse
	return response, c.call(ctx, http.MethodPost, "/v1/tasks/"+url.PathEscape(id)+"/decide", DecideRequest{Decision: decision}, &response)
}

func (c *Client) GetTask(ctx context.Context, id string) (domain.TaskSnapshot, error) {
	var task domain.TaskSnapshot
	return task, c.call(ctx, http.MethodGet, "/v1/tasks/"+url.PathEscape(id), nil, &task)
}

func (c *Client) GetReport(ctx context.Context, id string) (ReportResponse, error) {
	var report ReportResponse
	return report, c.call(ctx, http.MethodGet, "/v1/tasks/"+url.PathEscape(id)+"/report", nil, &report)
}

func (c *Client) GetExecution(ctx context.Context, id string) (domain.ExecutionSnapshot, error) {
	var execution domain.ExecutionSnapshot
	return execution, c.call(ctx, http.MethodGet, "/v1/executions/"+url.PathEscape(id), nil, &execution)
}

func (c *Client) ListNodes(ctx context.Context) ([]domain.NodeSnapshot, error) {
	nodes := []domain.NodeSnapshot{}
	return nodes, c.call(ctx, http.MethodGet, "/v1/nodes", nil, &nodes)
}

func (c *Client) GetNode(ctx context.Context, id string) (domain.NodeSnapshot, error) {
	var node domain.NodeSnapshot
	return node, c.call(ctx, http.MethodGet, "/v1/nodes/"+url.PathEscape(id), nil, &node)
}

// Register is the node protocol's registration (application.NodeTransport).
func (c *Client) Register(ctx context.Context, registration application.NodeRegistration) (domain.NodeSnapshot, error) {
	var node domain.NodeSnapshot
	request := nodeproto.RegisterRequest{ID: registration.ID, HostName: registration.HostName, Labels: registration.Labels, Capabilities: registration.Capabilities}
	return node, c.call(ctx, http.MethodPost, "/v1/nodes", request, &node)
}

// Heartbeat tells the control plane the node is alive.
func (c *Client) Heartbeat(ctx context.Context, node domain.NodeID) (domain.NodeSnapshot, error) {
	var snapshot domain.NodeSnapshot
	return snapshot, c.call(ctx, http.MethodPost, "/v1/nodes/"+url.PathEscape(node.String())+"/heartbeat", nil, &snapshot)
}

// NextAssignment long-polls for the node's next execution.
func (c *Client) NextAssignment(ctx context.Context, node domain.NodeID, wait time.Duration) (application.Assignment, bool, error) {
	var message nodeproto.AssignmentMessage
	path := "/v1/nodes/" + url.PathEscape(node.String()) + "/assignments/next?wait=" + url.QueryEscape(wait.String())
	found, err := c.callMaybe(ctx, http.MethodGet, path, nil, &message)
	if err != nil || !found {
		return application.Assignment{}, false, err
	}
	assignment, err := nodeproto.ToAssignment(message)
	if err != nil {
		return application.Assignment{}, false, fmt.Errorf("control plane sent a bad assignment: %w", err)
	}
	return assignment, true, nil
}

// RenewLease extends the node's lease on an execution.
func (c *Client) RenewLease(ctx context.Context, node domain.NodeID, execution domain.ExecutionID) (time.Time, error) {
	var lease nodeproto.LeaseMessage
	err := c.call(ctx, http.MethodPost, "/v1/nodes/"+url.PathEscape(node.String())+"/executions/"+url.PathEscape(execution.String())+"/lease", nil, &lease)
	return lease.LeaseUntil, err
}

// Report hands the execution's result to the control plane.
func (c *Client) Report(ctx context.Context, node domain.NodeID, execution domain.ExecutionID, report application.ExecutionReport) (application.ReportAck, error) {
	var ack nodeproto.ReportAck
	err := c.call(ctx, http.MethodPost, "/v1/nodes/"+url.PathEscape(node.String())+"/executions/"+url.PathEscape(execution.String())+"/report", nodeproto.FromReport(report), &ack)
	if err != nil {
		return application.ReportAck{}, err
	}
	outcome, err := domain.ParseExecutionOutcome(ack.Outcome)
	if err != nil {
		return application.ReportAck{}, err
	}
	return application.ReportAck{Accepted: ack.Accepted, Outcome: outcome}, nil
}

// callMaybe is call for an answer that may be 204 No Content: found is
// false then.
func (c *Client) callMaybe(ctx context.Context, method, path string, body, into any) (bool, error) {
	status, raw, err := c.do(ctx, method, path, body)
	if err != nil {
		return false, err
	}
	if status == http.StatusNoContent {
		return false, nil
	}
	if status >= 400 {
		var failure ErrorResponse
		_ = json.Unmarshal(raw, &failure)
		return false, fromWire(status, failure)
	}
	if err := json.Unmarshal(raw, into); err != nil {
		return false, fmt.Errorf("control plane answered %s: %w", method+" "+path, err)
	}
	return true, nil
}

// call sends one request and decodes the answer, or the error it carries.
func (c *Client) call(ctx context.Context, method, path string, body, into any) error {
	status, raw, err := c.do(ctx, method, path, body)
	if err != nil {
		return err
	}
	if status >= 400 {
		var failure ErrorResponse
		_ = json.Unmarshal(raw, &failure)
		return fromWire(status, failure)
	}
	if into == nil {
		return nil
	}
	if err := json.Unmarshal(raw, into); err != nil {
		return fmt.Errorf("control plane answered %s: %w", method+" "+path, err)
	}
	return nil
}

// do sends one request and returns the status and the body.
func (c *Client) do(ctx context.Context, method, path string, body any) (int, []byte, error) {
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return 0, nil, err
		}
		reader = bytes.NewReader(raw)
	}
	request, err := http.NewRequestWithContext(ctx, method, c.base+path, reader)
	if err != nil {
		return 0, nil, err
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := c.http.Do(request)
	if err != nil {
		return 0, nil, fmt.Errorf("control plane at %s: %w", c.address, err)
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, 16<<20))
	if err != nil {
		return 0, nil, err
	}
	return response.StatusCode, raw, nil
}
