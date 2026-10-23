package application

// BudgetsDefinition is the budgets section of a bot definition.
type BudgetsDefinition struct {
	MaxModelCallsPerTask int `json:"maxModelCallsPerTask,omitempty"`
	MaxToolCallsPerTask  int `json:"maxToolCallsPerTask,omitempty"`
	MaxDelegationDepth   int `json:"maxDelegationDepth,omitempty"`
	TaskTimeoutSeconds   int `json:"taskTimeoutSeconds,omitempty"`
}
