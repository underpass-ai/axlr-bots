package domain

// BudgetsSnapshot is Budgets as plain data; the timeout is in seconds.
type BudgetsSnapshot struct {
	MaxModelCallsPerTask int `json:"maxModelCallsPerTask"`
	MaxToolCallsPerTask  int `json:"maxToolCallsPerTask"`
	MaxDelegationDepth   int `json:"maxDelegationDepth"`
	TaskTimeoutSeconds   int `json:"taskTimeoutSeconds"`
}
