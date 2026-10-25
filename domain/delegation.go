package domain

import "math"

// Limits and defaults of a bot's delegation.
const (
	// DefaultDelegationDepth is the depth limit when neither the
	// delegation nor the budgets name one.
	DefaultDelegationDepth = 1
	// MaxDelegationDepthLimit is the deepest a chain may ever be allowed.
	MaxDelegationDepthLimit = 8
	// DefaultBudgetShare is the share of what is left of a task that a
	// child may take when the delegation names none.
	DefaultBudgetShare   = 0.5
	maxDelegationTargets = 16
)

// Delegation says which bots a bot may ask to do a task, how deep a
// chain through it may go and how much of what is left of its task a
// child may take. The zero value delegates to nobody: deny by default.
type Delegation struct {
	to          []BotID
	maxDepth    int
	budgetShare float64
}

// NewDelegation validates a delegation: up to 16 distinct targets, a
// depth limit from 1 to 8 (0 when not given) and a share in (0, 1]
// (0 when not given).
func NewDelegation(s DelegationSnapshot) (Delegation, error) {
	if len(s.To) > maxDelegationTargets {
		return Delegation{}, invalid("delegation to", "names at most 16 bots")
	}
	d := Delegation{maxDepth: s.MaxDepth, budgetShare: s.BudgetShare}
	seen := map[BotID]bool{}
	for _, raw := range s.To {
		id, err := NewBotID(raw)
		if err != nil {
			return Delegation{}, invalid("delegation to", err.Error())
		}
		if seen[id] {
			return Delegation{}, invalid("delegation to", "repeats "+id.String())
		}
		seen[id] = true
		d.to = append(d.to, id)
	}
	if s.MaxDepth < 0 || s.MaxDepth > MaxDelegationDepthLimit {
		return Delegation{}, invalid("delegation maxDepth", "must be from 1 to 8")
	}
	if math.IsNaN(s.BudgetShare) || s.BudgetShare < 0 || s.BudgetShare > 1 {
		return Delegation{}, invalid("delegation budgetShare", "must be more than 0 and at most 1")
	}
	return d, nil
}

// To lists the bots this bot may ask.
func (d Delegation) To() []BotID { return append([]BotID(nil), d.to...) }

// Enabled reports whether the bot may ask any bot.
func (d Delegation) Enabled() bool { return len(d.to) > 0 }

// Allows reports whether the bot may ask target.
func (d Delegation) Allows(target BotID) bool {
	for _, id := range d.to {
		if id == target {
			return true
		}
	}
	return false
}

// MaxDepth is the depth limit the definition names; 0 when it names none.
func (d Delegation) MaxDepth() int { return d.maxDepth }

// BudgetShare is the share a child may take; the default when not given.
func (d Delegation) BudgetShare() float64 {
	if d.budgetShare == 0 {
		return DefaultBudgetShare
	}
	return d.budgetShare
}

// Snapshot returns the delegation as plain data, nil when it is empty.
func (d Delegation) Snapshot() *DelegationSnapshot {
	if len(d.to) == 0 && d.maxDepth == 0 && d.budgetShare == 0 {
		return nil
	}
	to := make([]string, len(d.to))
	for i, id := range d.to {
		to[i] = id.String()
	}
	return &DelegationSnapshot{To: to, MaxDepth: d.maxDepth, BudgetShare: d.budgetShare}
}
