package domain

// PinnedLadderSnapshot is a LadderGrant as plain data, kept on the task.
type PinnedLadderSnapshot struct {
	Runbook   string `json:"runbook"`
	Tool      string `json:"tool"`
	Namespace string `json:"namespace"`
}

func (p ExecutionPolicy) ladderSnapshot() *PinnedLadderSnapshot {
	if p.grant == nil {
		return nil
	}
	return &PinnedLadderSnapshot{Runbook: p.grant.Runbook.String(), Tool: p.grant.Tool.String(), Namespace: p.grant.Namespace}
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
	return &LadderGrant{Runbook: runbook, Tool: tool, Namespace: namespace}, nil
}
