package domain

// DelegationSnapshot is a bot's delegation as plain data: the bots it
// may ask, the depth limit of a chain through it, the share of its task
// a child may take, how that is split among the children ("geometric"
// or "even") and how many children one task may ask for. Zero values
// mean "not given".
type DelegationSnapshot struct {
	To          []string `json:"to,omitempty"`
	MaxDepth    int      `json:"maxDepth,omitempty"`
	BudgetShare float64  `json:"budgetShare,omitempty"`
	Split       string   `json:"split,omitempty"`
	MaxChildren int      `json:"maxChildren,omitempty"`
}
