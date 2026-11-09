package nodeproto

// ModelReply is the model's answer to a ModelRequest.
type ModelReply struct {
	Text             string                 `json:"text,omitempty"`
	ToolCalls        []ModelToolCallMessage `json:"toolCalls,omitempty"`
	PromptTokens     int                    `json:"promptTokens"`
	CompletionTokens int                    `json:"completionTokens"`
}
