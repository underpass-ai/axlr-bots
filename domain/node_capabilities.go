package domain

// NodeCapabilities is what a node can run: the axlr-tui it has, whether
// that build takes an execution policy and the correlation ids, and
// whether the node is dedicated: it runs only the bots whose placement
// names it or selects its labels, never those placed on any node (a
// sidecar beside one service).
type NodeCapabilities struct {
	AXLRVersion          string `json:"axlrVersion,omitempty"`
	PolicySupported      bool   `json:"policySupported"`
	CorrelationSupported bool   `json:"correlationSupported"`
	Dedicated            bool   `json:"dedicated,omitempty"`
}
