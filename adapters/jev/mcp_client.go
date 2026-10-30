package jev

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// mcpProtocol is the Streamable HTTP protocol version the client speaks.
const mcpProtocol = "2025-06-18"

// maxMCPBody bounds a tool's answer.
const maxMCPBody = 4 << 20

// ToolError is a refusal the tool answered (isError true): what a model
// would read as the tool's result. A program reads it as a step that
// did not happen.
type ToolError struct {
	Tool string
	Text string
}

func (e ToolError) Error() string { return e.Tool + ": " + e.Text }

// ToolResult is what a tool answered: its text and, when the text is a
// JSON object, that object.
type ToolResult struct {
	Text   string
	Object map[string]any
}

// mcpClient calls one MCP server over Streamable HTTP: a POST per
// message, a JSON body per answer (the server never speaks first).
type mcpClient struct {
	url     string
	http    *http.Client
	session string
	next    int
}

func newMCPClient(url string, timeout time.Duration) *mcpClient {
	return &mcpClient{url: url, http: &http.Client{Timeout: timeout}}
}

// initialize opens the session, as the protocol asks before any call.
func (c *mcpClient) initialize(ctx context.Context, clientName, version string) error {
	var result struct {
		ProtocolVersion string `json:"protocolVersion"`
	}
	if err := c.call(ctx, "initialize", map[string]any{
		"protocolVersion": mcpProtocol, "capabilities": map[string]any{},
		"clientInfo": map[string]any{"name": clientName, "version": version},
	}, &result); err != nil {
		return err
	}
	if result.ProtocolVersion == "" {
		return errors.New("the MCP server answered initialize without a protocol version")
	}
	_ = c.notify(ctx, "notifications/initialized")
	return nil
}

// callTool calls one tool with its arguments.
func (c *mcpClient) callTool(ctx context.Context, name string, arguments map[string]any) (ToolResult, error) {
	if arguments == nil {
		arguments = map[string]any{}
	}
	var result struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if err := c.call(ctx, "tools/call", map[string]any{"name": name, "arguments": arguments}, &result); err != nil {
		return ToolResult{}, err
	}
	var text strings.Builder
	for _, content := range result.Content {
		if content.Type == "text" {
			text.WriteString(content.Text)
		}
	}
	if result.IsError {
		return ToolResult{}, ToolError{Tool: name, Text: text.String()}
	}
	out := ToolResult{Text: text.String()}
	trimmed := strings.TrimSpace(out.Text)
	if strings.HasPrefix(trimmed, "{") {
		_ = json.Unmarshal([]byte(trimmed), &out.Object)
	}
	return out, nil
}

func (c *mcpClient) notify(ctx context.Context, method string) error {
	_, err := c.post(ctx, map[string]any{"jsonrpc": "2.0", "method": method})
	return err
}

func (c *mcpClient) call(ctx context.Context, method string, params any, into any) error {
	c.next++
	id := c.next
	raw, err := c.post(ctx, map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
	if err != nil {
		return err
	}
	var response struct {
		ID     json.RawMessage `json:"id"`
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &response); err != nil {
		return fmt.Errorf("the MCP server's answer to %s is not JSON-RPC", method)
	}
	if response.Error != nil {
		return fmt.Errorf("the MCP server refused %s: %s (%d)", method, response.Error.Message, response.Error.Code)
	}
	if string(response.ID) != fmt.Sprint(id) {
		return fmt.Errorf("the MCP server answered another request than %s", method)
	}
	if into != nil && len(response.Result) > 0 {
		if err := json.Unmarshal(response.Result, into); err != nil {
			return fmt.Errorf("the MCP server's result of %s has another shape", method)
		}
	}
	return nil
}

func (c *mcpClient) post(ctx context.Context, body any) ([]byte, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json, text/event-stream")
	request.Header.Set("MCP-Protocol-Version", mcpProtocol)
	if c.session != "" {
		request.Header.Set("Mcp-Session-Id", c.session)
	}
	response, err := c.http.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("the MCP server at %s did not answer: %w", c.url, err)
	}
	defer response.Body.Close()
	if session := response.Header.Get("Mcp-Session-Id"); session != "" {
		c.session = session
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, maxMCPBody+1))
	if err != nil {
		return nil, fmt.Errorf("the MCP server at %s: %w", c.url, err)
	}
	if len(raw) > maxMCPBody {
		return nil, fmt.Errorf("the MCP server's answer is over %d bytes", maxMCPBody)
	}
	if response.StatusCode == http.StatusAccepted {
		return nil, nil
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("the MCP server at %s answered HTTP %d", c.url, response.StatusCode)
	}
	// A server that answers with an event stream sends the one message as
	// a data: line.
	if strings.HasPrefix(response.Header.Get("Content-Type"), "text/event-stream") {
		for _, line := range strings.Split(string(raw), "\n") {
			if strings.HasPrefix(line, "data:") {
				return []byte(strings.TrimSpace(strings.TrimPrefix(line, "data:"))), nil
			}
		}
		return nil, errors.New("the MCP server's event stream carried no message")
	}
	return raw, nil
}
