package nodeproto

// ModelRequest is what a node without a model key sends to
// POST /v1/nodes/{id}/executions/{exec}/model. It names no model: the
// control plane takes that from the execution's bot.
type ModelRequest struct {
	Messages []ModelChatMessage `json:"messages"`
	Tools    []ModelToolMessage `json:"tools,omitempty"`
}
