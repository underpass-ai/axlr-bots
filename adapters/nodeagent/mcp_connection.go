package nodeagent

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"sync"
)

// Limits of the MCP connection.
const (
	maxMCPMessageBytes = 1 << 20
	// mcpProtocolVersion is answered to a client that names none.
	mcpProtocolVersion = "2025-06-18"
)

// mcpConnection is one client of an mcpServer. Requests are answered as
// they finish, so a ping is answered while a delegation waits; a call
// ends when the client cancels it or the connection closes, which is
// what happens when AXLR stops the execution that made it.
type mcpConnection struct {
	server   *mcpServer
	conn     net.Conn
	writeMu  sync.Mutex
	mu       sync.Mutex
	inFlight map[string]context.CancelFunc
	calls    sync.WaitGroup
}

func (c *mcpConnection) serve(parent context.Context) {
	ctx, cancel := context.WithCancel(parent)
	stop := context.AfterFunc(ctx, func() { _ = c.conn.Close() })
	defer func() {
		cancel()
		stop()
		c.calls.Wait()
		_ = c.conn.Close()
	}()
	scanner := bufio.NewScanner(c.conn)
	scanner.Buffer(make([]byte, 0, 64*1024), maxMCPMessageBytes)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var request rpcRequest
		if err := json.Unmarshal(line, &request); err != nil {
			c.write(rpcResponse{JSONRPC: "2.0", ID: json.RawMessage("null"), Error: &rpcError{Code: rpcParseError, Message: "not a JSON-RPC message"}})
			continue
		}
		c.dispatch(ctx, request)
	}
}

func (c *mcpConnection) dispatch(ctx context.Context, request rpcRequest) {
	if request.JSONRPC != "2.0" || request.Method == "" {
		if !request.notification() {
			c.fail(request, rpcInvalidRequest, "not a JSON-RPC 2.0 request")
		}
		return
	}
	switch request.Method {
	case "initialize":
		var params struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		_ = json.Unmarshal(request.Params, &params)
		version := params.ProtocolVersion
		if version == "" {
			version = mcpProtocolVersion
		}
		c.reply(request, map[string]any{
			"protocolVersion": version,
			"capabilities":    map[string]any{"tools": map[string]any{"listChanged": false}},
			"serverInfo":      map[string]any{"name": "axlr-bots", "version": c.server.version},
		})
	case "ping":
		c.reply(request, map[string]any{})
	case "tools/list":
		c.reply(request, map[string]any{"tools": []mcpTool{c.server.tool}})
	case "tools/call":
		c.startCall(ctx, request)
	case "notifications/cancelled":
		var params struct {
			RequestID json.RawMessage `json:"requestId"`
		}
		if json.Unmarshal(request.Params, &params) == nil {
			c.cancelCall(string(params.RequestID))
		}
	default:
		if !request.notification() {
			c.fail(request, rpcMethodNotFound, "method "+request.Method+" is not served")
		}
	}
}

// startCall runs a tools/call on its own, so the connection keeps
// reading (a ping, a cancellation) while it waits.
func (c *mcpConnection) startCall(ctx context.Context, request rpcRequest) {
	var params struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(request.Params, &params); err != nil || params.Name != c.server.tool.Name {
		c.fail(request, rpcInvalidParams, "unknown tool "+params.Name)
		return
	}
	callCtx, cancel := context.WithCancel(ctx)
	key := string(request.ID)
	c.mu.Lock()
	c.inFlight[key] = cancel
	c.mu.Unlock()
	c.calls.Add(1)
	go func() {
		defer c.calls.Done()
		defer c.cancelCall(key)
		result := c.server.call(callCtx, params.Arguments)
		if callCtx.Err() != nil {
			// Cancelled or disconnected: nobody reads an answer.
			return
		}
		c.reply(request, result)
	}()
}

func (c *mcpConnection) cancelCall(key string) {
	c.mu.Lock()
	cancel, ok := c.inFlight[key]
	delete(c.inFlight, key)
	c.mu.Unlock()
	if ok {
		cancel()
	}
}

func (c *mcpConnection) reply(request rpcRequest, result any) {
	if request.notification() {
		return
	}
	c.write(rpcResponse{JSONRPC: "2.0", ID: request.ID, Result: result})
}

func (c *mcpConnection) fail(request rpcRequest, code int, message string) {
	c.write(rpcResponse{JSONRPC: "2.0", ID: request.ID, Error: &rpcError{Code: code, Message: message}})
}

func (c *mcpConnection) write(response rpcResponse) {
	data, err := json.Marshal(response)
	if err != nil {
		return
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	_, _ = c.conn.Write(append(data, '\n'))
}
