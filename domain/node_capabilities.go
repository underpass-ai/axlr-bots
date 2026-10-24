package domain

// NodeCapabilities is what a node can run: the axlr-tui it has and
// whether that build takes an execution policy and the correlation ids.
type NodeCapabilities struct {
	AXLRVersion          string `json:"axlrVersion,omitempty"`
	PolicySupported      bool   `json:"policySupported"`
	CorrelationSupported bool   `json:"correlationSupported"`
}
