package domain

import "time"

// Bot is a persistent, governed agent registered with the control plane:
// an identity, a mission and the contracts (tools, model, budgets) under
// which its tasks run. A bot is not a process; it exists between runs.
type Bot struct {
	id           BotID
	tenantID     TenantID
	name         BotName
	owner        Owner
	mission      Mission
	allowedTools AllowedTools
	modelPolicy  ModelPolicy
	budgets      Budgets
	status       BotStatus
	createdAt    time.Time
	updatedAt    time.Time
}

// NewBot creates a bot in Draft status.
func NewBot(id BotID, tenant TenantID, name BotName, owner Owner, mission Mission, tools AllowedTools, model ModelPolicy, budgets Budgets, now time.Time) (Bot, error) {
	if now.IsZero() {
		return Bot{}, invalid("created at", "must not be zero")
	}
	return Bot{
		id: id, tenantID: tenant, name: name, owner: owner, mission: mission,
		allowedTools: tools, modelPolicy: model, budgets: budgets,
		status: BotDraft, createdAt: now, updatedAt: now,
	}, nil
}

// ID is the bot's identifier.
func (b Bot) ID() BotID { return b.id }

// TenantID is the tenant the bot belongs to.
func (b Bot) TenantID() TenantID { return b.tenantID }

// Name is the bot's human name.
func (b Bot) Name() BotName { return b.name }

// Owner is who answers for the bot.
func (b Bot) Owner() Owner { return b.owner }

// Mission is the bot's declared responsibility.
func (b Bot) Mission() Mission { return b.mission }

// AllowedTools is the bot's tool allow-list.
func (b Bot) AllowedTools() AllowedTools { return b.allowedTools }

// ModelPolicy is how the bot's work picks a model.
func (b Bot) ModelPolicy() ModelPolicy { return b.modelPolicy }

// Budgets bounds each task of the bot.
func (b Bot) Budgets() Budgets { return b.budgets }

// Status is the bot's logical state.
func (b Bot) Status() BotStatus { return b.status }

// CreatedAt is when the bot was defined.
func (b Bot) CreatedAt() time.Time { return b.createdAt }

// UpdatedAt is when the bot last changed.
func (b Bot) UpdatedAt() time.Time { return b.updatedAt }

// Register moves a Draft bot to Registered.
func (b *Bot) Register(now time.Time) error {
	if b.status != BotDraft {
		return StateError{Subject: "bot " + b.id.String(), Current: b.status.String(), Attempted: "register"}
	}
	b.status = BotRegistered
	b.updatedAt = now
	return nil
}

// Enable lets a Registered or Paused bot accept tasks.
func (b *Bot) Enable(now time.Time) error {
	if b.status != BotRegistered && b.status != BotPaused {
		return StateError{Subject: "bot " + b.id.String(), Current: b.status.String(), Attempted: "enable"}
	}
	b.status = BotEnabled
	b.updatedAt = now
	return nil
}

// Pause puts an Enabled bot on hold.
func (b *Bot) Pause(now time.Time) error {
	if b.status != BotEnabled {
		return StateError{Subject: "bot " + b.id.String(), Current: b.status.String(), Attempted: "pause"}
	}
	b.status = BotPaused
	b.updatedAt = now
	return nil
}

// AcceptsTasks reports whether the bot may receive a task now.
func (b Bot) AcceptsTasks() bool { return b.status == BotEnabled }

// Snapshot returns the bot as plain data for storage and display.
func (b Bot) Snapshot() BotSnapshot {
	return BotSnapshot{
		ID:           b.id.String(),
		TenantID:     b.tenantID.String(),
		Name:         b.name.String(),
		Owner:        b.owner.String(),
		Mission:      b.mission.String(),
		AllowedTools: b.allowedTools.Names(),
		Model: ModelPolicySnapshot{
			Model:                b.modelPolicy.Model(),
			RoutingPolicy:        b.modelPolicy.Routing().String(),
			AllowRemoteProviders: b.modelPolicy.AllowRemoteProviders(),
			MaxContextTokens:     b.modelPolicy.MaxContextTokens(),
		},
		Budgets: BudgetsSnapshot{
			MaxModelCallsPerTask: b.budgets.MaxModelCallsPerTask(),
			MaxToolCallsPerTask:  b.budgets.MaxToolCallsPerTask(),
			MaxDelegationDepth:   b.budgets.MaxDelegationDepth(),
			TaskTimeoutSeconds:   int(b.budgets.TaskTimeout() / time.Second),
		},
		Status:    b.status.String(),
		CreatedAt: b.createdAt,
		UpdatedAt: b.updatedAt,
	}
}

// RestoreBot rebuilds a bot from a snapshot, validating every value.
func RestoreBot(s BotSnapshot) (Bot, error) {
	id, err := NewBotID(s.ID)
	if err != nil {
		return Bot{}, err
	}
	tenant, err := NewTenantID(s.TenantID)
	if err != nil {
		return Bot{}, err
	}
	name, err := NewBotName(s.Name)
	if err != nil {
		return Bot{}, err
	}
	owner, err := NewOwner(s.Owner)
	if err != nil {
		return Bot{}, err
	}
	mission, err := NewMission(s.Mission)
	if err != nil {
		return Bot{}, err
	}
	tools, err := NewAllowedTools(s.AllowedTools)
	if err != nil {
		return Bot{}, err
	}
	model, err := NewModelPolicy(s.Model.Model, s.Model.RoutingPolicy, s.Model.AllowRemoteProviders, s.Model.MaxContextTokens)
	if err != nil {
		return Bot{}, err
	}
	budgets, err := NewBudgets(s.Budgets.MaxModelCallsPerTask, s.Budgets.MaxToolCallsPerTask, s.Budgets.MaxDelegationDepth, time.Duration(s.Budgets.TaskTimeoutSeconds)*time.Second)
	if err != nil {
		return Bot{}, err
	}
	status, err := ParseBotStatus(s.Status)
	if err != nil {
		return Bot{}, err
	}
	bot, err := NewBot(id, tenant, name, owner, mission, tools, model, budgets, s.CreatedAt)
	if err != nil {
		return Bot{}, err
	}
	if s.UpdatedAt.IsZero() {
		return Bot{}, invalid("updated at", "must not be zero")
	}
	bot.status = status
	bot.updatedAt = s.UpdatedAt
	return bot, nil
}
