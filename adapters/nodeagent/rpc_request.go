package nodeagent

import "encoding/json"

// rpcRequest is one JSON-RPC 2.0 message from the MCP client: a request
// when it has an id, a notification when it has none.
type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// notification reports a message that expects no answer.
func (r rpcRequest) notification() bool { return len(r.ID) == 0 || string(r.ID) == "null" }
