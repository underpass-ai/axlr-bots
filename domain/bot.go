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
	level        AutonomyLevel
	toolRules    ToolRules
	triggers     Triggers
	placement    Placement
	approvals    Approvals
	status       BotStatus
	createdAt    time.Time
	updatedAt    time.Time
}

// NewBot creates a bot in Draft status at the observe level, with no
// tool rules; Govern raises the level or narrows the tools.
func NewBot(id BotID, tenant TenantID, name BotName, owner Owner, mission Mission, tools AllowedTools, model ModelPolicy, budgets Budgets, now time.Time) (Bot, error) {
	if now.IsZero() {
		return Bot{}, invalid("created at", "must not be zero")
	}
	return Bot{
		id: id, tenantID: tenant, name: name, owner: owner, mission: mission,
		allowedTools: tools, modelPolicy: model, budgets: budgets,
		level: AutonomyObserve, triggers: DefaultTriggers(), approvals: DefaultApprovals(), status: BotDraft, createdAt: now, updatedAt: now,
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

// Level is how far the bot may act on its own.
func (b Bot) Level() AutonomyLevel { return b.level }

// ToolRules narrows the allowed tools by their arguments.
func (b Bot) ToolRules() ToolRules { return b.toolRules }

// Triggers are the ways tasks reach the bot.
func (b Bot) Triggers() Triggers { return b.triggers }

// Placement says which nodes may run the bot's tasks.
func (b Bot) Placement() Placement { return b.placement }

// Approvals says how the bot's pending calls wait for a person.
func (b Bot) Approvals() Approvals { return b.approvals }

// SetApprovals replaces how the bot's pending calls wait for a person.
func (b *Bot) SetApprovals(approvals Approvals) { b.approvals = approvals }

// AcceptsTrigger reports whether a task of the type may reach the bot.
func (b Bot) AcceptsTrigger(kind TriggerType) bool { return b.triggers.Has(kind) }

// Trigger finds one of the bot's triggers by name.
func (b Bot) Trigger(name string) (Trigger, bool) { return b.triggers.Named(name) }

// Wire sets how tasks reach the bot and where they run.
func (b *Bot) Wire(triggers Triggers, placement Placement) {
	b.triggers = triggers
	b.placement = placement
}

// Redefine replaces the bot's mission and contracts. The tool rules must
// still name allowed tools: a narrower allow-list that strands a rule is
// refused, so Govern the rules first when both change.
func (b *Bot) Redefine(mission Mission, tools AllowedTools, model ModelPolicy, budgets Budgets) error {
	for _, rule := range b.toolRules.Allow() {
		if !tools.Allows(rule.Tool()) {
			return invalid("allowed tools", "an allow rule names "+rule.Tool().String()+", which the new list leaves out")
		}
	}
	b.mission, b.allowedTools, b.modelPolicy, b.budgets = mission, tools, model, budgets
	return nil
}

// Touch records that the bot's definition changed.
func (b *Bot) Touch(now time.Time) { b.updatedAt = now }

// Govern sets the bot's autonomy level and tool rules. The rules must
// name allowed tools; the level alone decides nothing about which tools
// exist, only what the allowed ones may do.
func (b *Bot) Govern(level AutonomyLevel, rules ToolRules) error {
	parsed, err := ParseAutonomyLevel(level.String())
	if err != nil {
		return err
	}
	for _, rule := range rules.Allow() {
		if !b.allowedTools.Allows(rule.Tool()) {
			return invalid("tool rules", "allow rule names "+rule.Tool().String()+", which is not an allowed tool")
		}
	}
	b.level = parsed
	b.toolRules = rules
	return nil
}

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
		Level:     b.level.String(),
		ToolRules: b.toolRules.Snapshot(),
		Triggers:  b.triggers.Snapshot(),
		Placement: b.placement.Snapshot(),
		Approvals: b.approvals.Snapshot(),
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
	level, err := ParseAutonomyLevel(s.Level)
	if err != nil {
		return Bot{}, err
	}
	rules, err := RestoreToolRules(s.ToolRules, tools)
	if err != nil {
		return Bot{}, err
	}
	bot, err := NewBot(id, tenant, name, owner, mission, tools, model, budgets, s.CreatedAt)
	if err != nil {
		return Bot{}, err
	}
	if err := bot.Govern(level, rules); err != nil {
		return Bot{}, err
	}
	triggers, err := NewTriggers(s.Triggers)
	if err != nil {
		return Bot{}, err
	}
	placement, err := NewPlacement(s.Placement.Nodes, s.Placement.Selector)
	if err != nil {
		return Bot{}, err
	}
	bot.Wire(triggers, placement)
	approvals, err := NewApprovals(s.Approvals)
	if err != nil {
		return Bot{}, err
	}
	bot.SetApprovals(approvals)
	if s.UpdatedAt.IsZero() {
		return Bot{}, invalid("updated at", "must not be zero")
	}
	bot.status = status
	bot.updatedAt = s.UpdatedAt
	return bot, nil
}
