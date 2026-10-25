package nodeproto

// DelegationOfferMessage tells the node an execution's bot may delegate:
// the bots it may ask, the task's depth and the chain's limit. A node
// older than the field ignores it and offers no tool.
type DelegationOfferMessage struct {
	Targets  []string `json:"targets"`
	Depth    int      `json:"depth"`
	MaxDepth int      `json:"maxDepth"`
}
