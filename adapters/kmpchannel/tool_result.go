package kmpchannel

// toolResult is what a tools/call answers.
type toolResult struct {
	IsError bool `json:"isError"`
	Content []struct {
		Text string `json:"text"`
	} `json:"content"`
}
