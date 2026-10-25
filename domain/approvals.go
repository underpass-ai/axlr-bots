package domain

import (
	"strconv"
	"time"
)

// Bounds of an approval's time to live.
const (
	DefaultApprovalTTL = time.Hour
	MinApprovalTTL     = time.Minute
	MaxApprovalTTL     = 30 * 24 * time.Hour
	maxNotifyTargets   = 8
)

// Approvals is how a bot's pending calls wait for a person: for how long,
// what happens when nobody decides in time and who is told (vision §14:
// an expired approval does not authorise the operation).
type Approvals struct {
	ttl      time.Duration
	onExpire ApprovalExpiry
	notify   []NotifyTarget
}

// DefaultApprovals is what a definition without approvals means: an
// hour, then deny, nobody notified.
func DefaultApprovals() Approvals {
	return Approvals{ttl: DefaultApprovalTTL, onExpire: ExpireDeny}
}

// NewApprovals validates the settings; an empty field takes its default.
func NewApprovals(s ApprovalsSnapshot) (Approvals, error) {
	a := DefaultApprovals()
	if s.TTL != "" {
		ttl, err := time.ParseDuration(s.TTL)
		if err != nil {
			return Approvals{}, invalid("approvals ttl", "must be a duration such as 1h or 15m")
		}
		if ttl < MinApprovalTTL || ttl > MaxApprovalTTL {
			return Approvals{}, invalid("approvals ttl", "must be between 1m and 720h")
		}
		a.ttl = ttl
	}
	onExpire, err := ParseApprovalExpiry(s.OnExpire)
	if err != nil {
		return Approvals{}, err
	}
	a.onExpire = onExpire
	if len(s.Notify) > maxNotifyTargets {
		return Approvals{}, invalid("approvals notify", "takes at most 8 targets")
	}
	seen := map[string]bool{}
	for _, definition := range s.Notify {
		target, err := NewNotifyTarget(definition)
		if err != nil {
			return Approvals{}, err
		}
		if seen[target.Name()] {
			return Approvals{}, invalid("approvals notify", "repeat the name "+target.Name())
		}
		seen[target.Name()] = true
		a.notify = append(a.notify, target)
	}
	return a, nil
}

// TTL is how long a pending call waits for a person.
func (a Approvals) TTL() time.Duration {
	if a.ttl == 0 {
		return DefaultApprovalTTL
	}
	return a.ttl
}

// OnExpire is what happens to a call nobody decided in time.
func (a Approvals) OnExpire() ApprovalExpiry {
	if a.onExpire == "" {
		return ExpireDeny
	}
	return a.onExpire
}

// Targets are who is told when a task starts to wait, copied.
func (a Approvals) Targets() []NotifyTarget { return append([]NotifyTarget(nil), a.notify...) }

// Target finds a target by name.
func (a Approvals) Target(name string) (NotifyTarget, bool) {
	for _, target := range a.notify {
		if target.Name() == name {
			return target, true
		}
	}
	return NotifyTarget{}, false
}

// Snapshot returns the settings as plain data, secrets included.
func (a Approvals) Snapshot() ApprovalsSnapshot {
	s := ApprovalsSnapshot{TTL: formatTTL(a.TTL()), OnExpire: a.OnExpire().String()}
	for _, target := range a.notify {
		s.Notify = append(s.Notify, target.Snapshot())
	}
	return s
}

// formatTTL writes a duration the way an operator would: 1h, 2m, 90s.
func formatTTL(d time.Duration) string {
	switch {
	case d%time.Hour == 0:
		return strconv.FormatInt(int64(d/time.Hour), 10) + "h"
	case d%time.Minute == 0:
		return strconv.FormatInt(int64(d/time.Minute), 10) + "m"
	}
	return d.String()
}
