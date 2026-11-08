package domain

// LadderGrant is what an apply task may do alone: the one write tool of
// its runbook, in its incident's namespace, pinned on the task when it
// launches.
type LadderGrant struct {
	Runbook   RunbookName
	Tool      ToolName
	Namespace string
}

// namespaceArgument is the argument a ladder's bound is rendered on.
const namespaceArgument = "namespace"

// boundRule is the allow rule that holds the write tool to the namespace.
func (g LadderGrant) boundRule() (ToolRule, error) {
	return NewToolRule(g.Tool.String(), map[string]string{namespaceArgument: g.Namespace})
}
