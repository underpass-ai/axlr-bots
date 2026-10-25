package nodeagent

import "encoding/json"

// mcpToolResult is what tools/call answers: the text the model reads,
// and isError when the call did not do what it asked (a refusal, a
// control plane that could not be reached).
type mcpToolResult struct {
	Content []mcpContent `json:"content"`
	IsError bool         `json:"isError"`
}

// textResult is a result of one text block.
func textResult(text string, isError bool) mcpToolResult {
	return mcpToolResult{Content: []mcpContent{{Type: "text", Text: text}}, IsError: isError}
}

// jsonResult is a result whose text is v as JSON.
func jsonResult(v any, isError bool) mcpToolResult {
	data, err := json.Marshal(v)
	if err != nil {
		return textResult("the node could not encode the answer: "+err.Error(), true)
	}
	return textResult(string(data), isError)
}
