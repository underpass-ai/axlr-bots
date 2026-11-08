package domain

// PinnedLadderSnapshot is a LadderGrant as plain data, kept on the task.
type PinnedLadderSnapshot struct {
	Runbook      string `json:"runbook"`
	Tool         string `json:"tool"`
	Namespace    string `json:"namespace"`
	Alone        bool   `json:"alone,omitempty"`
	MaxToolCalls int    `json:"maxToolCalls,omitempty"`
}

func (p ExecutionPolicy) ladderSnapshot() *PinnedLadderSnapshot {
	if p.grant == nil {
		return nil
	}
	return &PinnedLadderSnapshot{Runbook: p.grant.Runbook.String(), Tool: p.grant.Tool.String(), Namespace: p.grant.Namespace, Alone: p.grant.Alone, MaxToolCalls: p.grant.MaxToolCalls}
}

func restoreLadderGrant(s *PinnedLadderSnapshot) (*LadderGrant, error) {
	if s == nil {
		return nil, nil
	}
	runbook, err := NewRunbookName(s.Runbook)
	if err != nil {
		return nil, err
	}
	tool, err := NewToolName(s.Tool)
	if err != nil {
		return nil, err
	}
	namespace, err := validateIdentifier("pinned ladder namespace", s.Namespace)
	if err != nil {
		return nil, err
	}
	if s.MaxToolCalls < 0 {
		return nil, invalid("pinned ladder", "maxToolCalls must not be negative")
	}
	return &LadderGrant{Runbook: runbook, Tool: tool, Namespace: namespace, Alone: s.Alone, MaxToolCalls: s.MaxToolCalls}, nil
}
