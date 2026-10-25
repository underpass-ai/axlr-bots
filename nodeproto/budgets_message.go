package nodeproto

// BudgetsMessage is what an execution may spend beyond its timeout: the
// model calls and the tool calls (0: no limit). The node passes them to
// AXLR in --budget.
type BudgetsMessage struct {
	MaxModelCalls int `json:"maxModelCalls,omitempty"`
	MaxToolCalls  int `json:"maxToolCalls,omitempty"`
}
