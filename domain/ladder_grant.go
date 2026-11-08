package domain

// LadderMaxToolCalls is the most a run acting alone may call: its reads
// and its one write (design §8.1, one apply per task).
const LadderMaxToolCalls = 6

// LadderGrant is the unit an apply task belongs to, pinned on it when it
// launches: the runbook, its one write tool and the incident's namespace.
// Its signals fall on this unit whatever the incident says later. Alone
// is what a ladder lets the task do without a card, and MaxToolCalls how
// many calls it may make.
type LadderGrant struct {
	Runbook      RunbookName
	Tool         ToolName
	Namespace    string
	Alone        bool
	MaxToolCalls int
}

// namespaceArgument is the argument a ladder's bound is rendered on.
const namespaceArgument = "namespace"

// boundRule is the allow rule that holds the write tool to the namespace.
func (g LadderGrant) boundRule() (ToolRule, error) {
	return NewToolRule(g.Tool.String(), map[string]string{namespaceArgument: g.Namespace})
}
