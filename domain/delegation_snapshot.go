package domain

// DelegationSnapshot is a bot's delegation as plain data: the bots it
// may ask, the depth limit of a chain through it, the share of its task
// a child may take, how that is split among the children ("geometric"
// or "even"), how many children one task may ask for and whether it
// takes tasks its teammates delegate (fromTeam; absent: it does). Zero
// values mean "not given".
type DelegationSnapshot struct {
	To          []string `json:"to,omitempty"`
	MaxDepth    int      `json:"maxDepth,omitempty"`
	BudgetShare float64  `json:"budgetShare,omitempty"`
	Split       string   `json:"split,omitempty"`
	MaxChildren int      `json:"maxChildren,omitempty"`
	FromTeam    *bool    `json:"fromTeam,omitempty"`
}
