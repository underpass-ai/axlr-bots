package application

// ModelAnswer is what the model answered to a proxied call.
type ModelAnswer struct {
	Text             string
	ToolCalls        []ModelToolCall
	PromptTokens     int
	CompletionTokens int
}
