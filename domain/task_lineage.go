package domain

// TaskLineage is where a delegated task came from: the task, execution
// and bot that asked for it, its depth (1 for a child of a task nobody
// delegated), the depth limit of its chain, the bots above it (the
// chain, root first, its parent's bot last) and what it was granted.
type TaskLineage struct {
	ParentTaskID      string          `json:"parentTaskId"`
	ParentExecutionID string          `json:"parentExecutionId"`
	ParentBotID       string          `json:"parentBotId"`
	Depth             int             `json:"depth"`
	MaxDepth          int             `json:"maxDepth"`
	Chain             []string        `json:"chain"`
	Grant             DelegationGrant `json:"grant"`
}

// Validate checks the lineage's invariants.
func (l TaskLineage) Validate() error {
	if _, err := NewTaskID(l.ParentTaskID); err != nil {
		return invalid("delegation parent task", err.Error())
	}
	if _, err := NewExecutionID(l.ParentExecutionID); err != nil {
		return invalid("delegation parent execution", err.Error())
	}
	parent, err := NewBotID(l.ParentBotID)
	if err != nil {
		return invalid("delegation parent bot", err.Error())
	}
	if l.Depth < 1 || l.Depth > l.MaxDepth || l.MaxDepth > MaxDelegationDepthLimit {
		return invalid("delegation depth", "must be from 1 to the chain's limit, at most 8")
	}
	if len(l.Chain) != l.Depth {
		return invalid("delegation chain", "names one bot per level above the task")
	}
	for _, raw := range l.Chain {
		if _, err := NewBotID(raw); err != nil {
			return invalid("delegation chain", err.Error())
		}
	}
	if l.Chain[len(l.Chain)-1] != parent.String() {
		return invalid("delegation chain", "ends with the parent's bot")
	}
	return l.Grant.Validate()
}

// Visited reports whether the bot is on the chain above the task.
func (l TaskLineage) Visited(bot BotID) bool {
	for _, raw := range l.Chain {
		if raw == bot.String() {
			return true
		}
	}
	return false
}
