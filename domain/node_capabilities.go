package domain

// NodeCapabilities is what a node can run: the axlr-tui it has, whether
// that build takes an execution policy and the correlation ids, and
// whether the node is dedicated: it runs only the bots whose placement
// names it or selects its labels, never those placed on any node (a
// sidecar beside one service), and the runtimes it has beside AXLR (a
// node that names none runs AXLR bots only).
type NodeCapabilities struct {
	AXLRVersion          string   `json:"axlrVersion,omitempty"`
	PolicySupported      bool     `json:"policySupported"`
	CorrelationSupported bool     `json:"correlationSupported"`
	Dedicated            bool     `json:"dedicated,omitempty"`
	Runtimes             []string `json:"runtimes,omitempty"`
}

// Runs reports whether the node runs bots of the runtime: every node
// runs AXLR; another runtime only when the node named it.
func (c NodeCapabilities) Runs(kind RuntimeKind) bool {
	if kind == RuntimeAXLR {
		return true
	}
	for _, runtime := range c.Runtimes {
		if runtime == kind.String() {
			return true
		}
	}
	return false
}
