package domain

// DelegationSnapshot is a bot's delegation as plain data: the bots it
// may ask, the depth limit of a chain through it and the share of what
// is left of its task a child may take. Zero values mean "not given".
type DelegationSnapshot struct {
	To          []string `json:"to,omitempty"`
	MaxDepth    int      `json:"maxDepth,omitempty"`
	BudgetShare float64  `json:"budgetShare,omitempty"`
}
