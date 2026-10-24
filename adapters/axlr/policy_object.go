package axlr

// policyObject is the contract's policy: a level and its tool lists.
type policyObject struct {
	Level string      `json:"level"`
	Tools policyTools `json:"tools"`
}
