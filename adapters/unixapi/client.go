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
	"time"

	"github.com/underpass-ai/underpass-bots/application"
	"github.com/underpass-ai/underpass-bots/domain"
)

// Client is the Service over the daemon's Unix socket.
type Client struct {
	http *http.Client
}

// NewClient talks to the daemon on the socket. Runs can take as long as
// a task, so the client sets no overall timeout; the caller's context
// bounds a call.
func NewClient(socketPath string) *Client {
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var dialer net.Dialer
			return dialer.DialContext(ctx, "unix", socketPath)
		},
	}
	return &Client{http: &http.Client{Transport: transport}}
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

func (c *Client) Alert(ctx context.Context, request AlertRequest) (SubmitResponse, error) {
	var response SubmitResponse
	return response, c.call(ctx, http.MethodPost, "/v1/alerts", request, &response)
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

// call sends one request and decodes the answer, or the error it carries.
func (c *Client) call(ctx context.Context, method, path string, body, into any) error {
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(raw)
	}
	request, err := http.NewRequestWithContext(ctx, method, "http://underpass-bots"+path, reader)
	if err != nil {
		return err
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := c.http.Do(request)
	if err != nil {
		return fmt.Errorf("control plane socket: %w", err)
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, 16<<20))
	if err != nil {
		return err
	}
	if response.StatusCode >= 400 {
		var failure ErrorResponse
		_ = json.Unmarshal(raw, &failure)
		return fromWire(response.StatusCode, failure)
	}
	if into == nil {
		return nil
	}
	if err := json.Unmarshal(raw, into); err != nil {
		return fmt.Errorf("control plane answered %s: %w", method+" "+path, err)
	}
	return nil
}
