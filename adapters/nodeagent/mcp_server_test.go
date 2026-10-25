package nodeagent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/underpass-ai/axlr-bots/application"
)

func TestMCPServerAnswersTheProtocol(t *testing.T) {
	path := shortDir(t) + "/x.sock"
	started := make(chan struct{}, 1)
	cancelled := make(chan struct{}, 1)
	tool := delegateTool(application.DelegationOffer{Targets: []string{"bot-b"}, Depth: 0, MaxDepth: 1})
	server, err := serveMCP(context.Background(), path, tool, "v1", func(ctx context.Context, raw json.RawMessage) mcpToolResult {
		if strings.Contains(string(raw), "block") {
			started <- struct{}{}
			<-ctx.Done()
			cancelled <- struct{}{}
			return textResult("late", true)
		}
		return textResult("echo "+string(raw), false)
	})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	c := dialMCP(t, path)
	defer c.conn.Close()
	answer, _ := c.request("initialize", map[string]any{"protocolVersion": "2025-03-26"})
	result := answer["result"].(map[string]any)
	if result["protocolVersion"] != "2025-03-26" || result["serverInfo"].(map[string]any)["name"] != "axlr-bots" {
		t.Errorf("initialize = %v", answer)
	}
	if answer, _ = c.request("initialize", map[string]any{}); answer["result"].(map[string]any)["protocolVersion"] != mcpProtocolVersion {
		t.Errorf("default version = %v", answer)
	}
	if answer, _ = c.request("ping", nil); answer["result"] == nil {
		t.Errorf("ping = %v", answer)
	}
	answer, _ = c.request("tools/list", nil)
	tools := answer["result"].(map[string]any)["tools"].([]any)
	listed := tools[0].(map[string]any)
	schema := listed["inputSchema"].(map[string]any)
	if listed["name"] != application.DelegateToolName || !strings.Contains(listed["description"].(string), "bot-b") || schema["additionalProperties"] != false {
		t.Errorf("tools = %v", tools)
	}
	if enum := schema["properties"].(map[string]any)["bot"].(map[string]any)["enum"].([]any); len(enum) != 1 || enum[0] != "bot-b" {
		t.Errorf("enum = %v", enum)
	}
	answer, _ = c.request("tools/call", map[string]any{"name": application.DelegateToolName, "arguments": map[string]any{"bot": "bot-b"}})
	if text := answer["result"].(map[string]any)["content"].([]any)[0].(map[string]any)["text"]; !strings.Contains(text.(string), "bot-b") {
		t.Errorf("call = %v", answer)
	}
	if answer, _ = c.request("tools/call", map[string]any{"name": "other"}); answer["error"].(map[string]any)["code"] != float64(rpcInvalidParams) {
		t.Errorf("unknown tool = %v", answer)
	}
	if answer, _ = c.request("resources/list", nil); answer["error"].(map[string]any)["code"] != float64(rpcMethodNotFound) {
		t.Errorf("unknown method = %v", answer)
	}
	// A notification of an unknown kind, a blank line and garbage.
	c.send(`{"jsonrpc":"2.0","method":"notifications/progress"}`)
	c.send("")
	c.send("not json")
	if !c.scanner.Scan() || !strings.Contains(c.scanner.Text(), `"code":-32700`) {
		t.Errorf("parse error = %s", c.scanner.Text())
	}
	c.send(`{"jsonrpc":"1.0","id":99,"method":"ping"}`)
	if !c.scanner.Scan() || !strings.Contains(c.scanner.Text(), `"code":-32600`) {
		t.Errorf("invalid request = %s", c.scanner.Text())
	}
	// A call in flight is cancelled by the client, and a ping is
	// answered meanwhile.
	c.send(`{"jsonrpc":"2.0","id":"slow","method":"tools/call","params":{"name":"axlr_bots_delegate","arguments":{"block":true}}}`)
	<-started
	if answer, _ = c.request("ping", nil); answer["result"] == nil {
		t.Errorf("ping during a call = %v", answer)
	}
	c.send(`{"jsonrpc":"2.0","method":"notifications/cancelled","params":{"requestId":"slow"}}`)
	select {
	case <-cancelled:
	case <-time.After(5 * time.Second):
		t.Fatal("the cancelled call kept running")
	}
	// A call in flight ends when the connection closes.
	c.send(`{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"axlr_bots_delegate","arguments":{"block":true}}}`)
	<-started
	_ = c.conn.Close()
	select {
	case <-cancelled:
	case <-time.After(5 * time.Second):
		t.Fatal("a call outlived its connection")
	}
}

func TestMCPServerReplacesAStaleSocketAndRemovesItsOwn(t *testing.T) {
	dir := shortDir(t)
	path := dir + "/x.sock"
	first, err := serveMCP(context.Background(), path, mcpTool{Name: "t"}, "v", func(context.Context, json.RawMessage) mcpToolResult { return textResult("", false) })
	if err != nil {
		t.Fatal(err)
	}
	second, err := serveMCP(context.Background(), path, mcpTool{Name: "t"}, "v", func(context.Context, json.RawMessage) mcpToolResult { return textResult("", false) })
	if err != nil {
		t.Fatalf("stale socket: %v", err)
	}
	second.Close()
	first.Close()
	if _, err := serveMCP(context.Background(), dir+"/missing/x.sock", mcpTool{}, "v", nil); err == nil {
		t.Error("listened in a missing directory")
	}
	for _, bad := range []string{dir + "/../x.sock", dir + "/x.json"} {
		if _, err := serveMCP(context.Background(), bad, mcpTool{}, "v", nil); err == nil {
			t.Errorf("listened on %s", bad)
		}
	}
	agent := Agent{Options: Options{SocketDir: dir}, slots: newSlots(1)}
	if socket, closeSocket := agent.serveDelegation(context.Background(), "web-1", "exec-..", &application.DelegationOffer{Targets: []string{"b"}}); socket != "" {
		closeSocket()
		t.Errorf("served %s", socket)
	}
}

func TestSlotsLendAndReclaim(t *testing.T) {
	s := newSlots(0)
	ctx := context.Background()
	if !s.acquire(ctx) || s.busy() != 1 {
		t.Fatal("first slot")
	}
	short, cancel := context.WithTimeout(ctx, 20*time.Millisecond)
	defer cancel()
	if s.acquire(short) {
		t.Fatal("a second slot while one works")
	}
	s.lend()
	if !s.acquire(ctx) || s.busy() != 1 {
		t.Fatal("the lent slot")
	}
	s.reclaim()
	if s.busy() != 2 {
		t.Errorf("reclaim over the limit: %d", s.busy())
	}
	got := make(chan bool, 1)
	go func() { got <- s.acquire(ctx) }()
	s.release()
	s.release()
	if !<-got {
		t.Error("a freed slot was not taken")
	}
}
