package domain

import "time"

// BotSnapshot is a bot as plain data: what repositories store and the CLI
// prints. RestoreBot turns it back into a validated Bot.
type BotSnapshot struct {
	ID            string                    `json:"id"`
	TenantID      string                    `json:"tenantId"`
	Name          string                    `json:"name"`
	Owner         string                    `json:"owner"`
	Mission       string                    `json:"mission"`
	AllowedTools  []string                  `json:"allowedTools"`
	Model         ModelPolicySnapshot       `json:"model"`
	Budgets       BudgetsSnapshot           `json:"budgets"`
	Level         string                    `json:"level"`
	ToolRules     ToolRulesSnapshot         `json:"toolRules"`
	Triggers      []TriggerSnapshot         `json:"triggers"`
	Placement     PlacementSnapshot         `json:"placement"`
	Approvals     ApprovalsSnapshot         `json:"approvals"`
	Delegation    *DelegationSnapshot       `json:"delegation,omitempty"`
	Groups        []string                  `json:"groups,omitempty"`
	Events        *EventsSnapshot           `json:"events,omitempty"`
	Subscriptions []SubscriptionSnapshot    `json:"subscriptions,omitempty"`
	Runtime       *RuntimeSnapshot          `json:"runtime,omitempty"`
	Skills        []SkillPinSnapshot        `json:"skills,omitempty"`
	Version       string                    `json:"version,omitempty"`
	Staged        *StagedDefinitionSnapshot `json:"staged,omitempty"`
	Status        string                    `json:"status"`
	CreatedAt     time.Time                 `json:"createdAt"`
	UpdatedAt     time.Time                 `json:"updatedAt"`
}

// Redacted is the snapshot with every trigger credential and every
// notification secret hidden, and the staged change reduced to its
// version and time (the change may carry both): what the API answers
// and the command prints. The repository keeps the unredacted snapshot;
// RestoreBot refuses a redacted one as a secret.
func (s BotSnapshot) Redacted() BotSnapshot {
	triggers := make([]TriggerSnapshot, len(s.Triggers))
	for i, t := range s.Triggers {
		triggers[i] = t.Redacted()
	}
	s.Triggers = triggers
	s.Approvals = s.Approvals.Redacted()
	if s.Staged != nil {
		s.Staged = &StagedDefinitionSnapshot{Version: s.Staged.Version, StagedAt: s.Staged.StagedAt}
	}
	return s
}
