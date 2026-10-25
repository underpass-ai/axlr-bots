package nodeagent

// JSON-RPC 2.0 error codes the server answers with.
const (
	rpcParseError     = -32700
	rpcInvalidRequest = -32600
	rpcMethodNotFound = -32601
	rpcInvalidParams  = -32602
)

// rpcError is a protocol error: a message the server could not read or
// a method it does not serve. A refused delegation is not one: it is a
// tool result with isError, which the model reads.
type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}
