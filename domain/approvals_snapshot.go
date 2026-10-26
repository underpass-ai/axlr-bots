package domain

// ApprovalsSnapshot is a bot's approval settings as plain data: how long
// a pending call waits for a person (a duration such as 1h, the default),
// what happens when nobody decides it in time (deny, the default, or
// cancel), the targets notified when a task starts to wait, and the
// calls the control plane approves without a person (autoApprove).
type ApprovalsSnapshot struct {
	TTL         string                 `json:"ttl,omitempty"`
	OnExpire    string                 `json:"onExpire,omitempty"`
	Notify      []NotifyTargetSnapshot `json:"notify,omitempty"`
	AutoApprove []ToolRuleSnapshot     `json:"autoApprove,omitempty"`
}

// Redacted is the snapshot with every target's secret hidden.
func (s ApprovalsSnapshot) Redacted() ApprovalsSnapshot {
	if s.Notify == nil {
		return s
	}
	targets := make([]NotifyTargetSnapshot, len(s.Notify))
	for i, t := range s.Notify {
		targets[i] = t.Redacted()
	}
	s.Notify = targets
	return s
}
