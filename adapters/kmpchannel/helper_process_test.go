package kmpchannel

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

// TestHelperProcess is the fake kmp-mcp: the writer runs this test binary
// with FAKE_KMP naming a scenario, and it serves MCP on stdio as the
// scenario says. It records the store, the viewer and the tool call it
// got in FAKE_KMP_OUT.
func TestHelperProcess(t *testing.T) {
	scenario := os.Getenv("FAKE_KMP")
	if scenario == "" {
		return
	}
	defer os.Exit(0)
	fmt.Fprintln(os.Stderr, "kmp-mcp: using embedded backend")
	if scenario == "exit" {
		fmt.Fprintln(os.Stderr, "kmp-mcp: the store is locked")
		os.Exit(3)
	}
	in := bufio.NewScanner(os.Stdin)
	in.Buffer(make([]byte, 1<<20), 1<<20)
	for in.Scan() {
		var message struct {
			ID     *int            `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if json.Unmarshal(in.Bytes(), &message) != nil || message.ID == nil {
			continue
		}
		id := *message.ID
		switch message.Method {
		case "initialize":
			if scenario == "rpcerror" {
				fmt.Printf(`{"jsonrpc":"2.0","id":%d,"error":{"code":-32600,"message":"unsupported protocol"}}`+"\n", id)
				continue
			}
			// A notification and another id first: the client skips both.
			fmt.Println(`{"jsonrpc":"2.0","method":"notifications/message","params":{}}`)
			fmt.Println(`{"jsonrpc":"2.0","id":99,"result":{}}`)
			fmt.Println("not json")
			fmt.Printf(`{"jsonrpc":"2.0","id":%d,"result":{"capabilities":{"tools":{}},"instructions":"%s"}}`+"\n", id, strings.Repeat("x", 100<<10))
		case "tools/call":
			if scenario == "hang" {
				time.Sleep(time.Minute)
			}
			if out := os.Getenv("FAKE_KMP_OUT"); out != "" {
				record, _ := json.Marshal(map[string]any{"store": os.Getenv("KMP_MCP_DATA_DIR"), "viewer": os.Getenv("KMP_VIEWER_ADDR"), "params": message.Params})
				_ = os.WriteFile(out, record, 0o600)
			}
			if scenario == "refuse" {
				fmt.Printf(`{"jsonrpc":"2.0","id":%d,"result":{"isError":true,"content":[{"type":"text","text":"idempotency key 'canal:answered:task-q1' was already accepted with different content\nUsage help: …"}]}}`+"\n", id)
				continue
			}
			if scenario == "garbled" {
				fmt.Printf(`{"jsonrpc":"2.0","id":%d,"result":"not an object"}`+"\n", id)
				continue
			}
			fmt.Printf(`{"jsonrpc":"2.0","id":%d,"result":{"isError":false,"content":[{"type":"text","text":"Ingested 1 entry, 0 relations, and 1 evidence item for project:dev.canal."}]}}`+"\n", id)
			if scenario == "fail-after" {
				os.Exit(2)
			}
		}
	}
}
