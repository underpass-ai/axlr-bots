package application

import (
	"context"
	"time"

	"github.com/underpass-ai/underpass-bots/domain"
)

// RegisterBot validates a definition, stores the bot as Registered and
// publishes bot.registered. The id defaults to the name when the file
// gives none.
type RegisterBot struct {
	deps Dependencies
	rec  recorder
}

// NewRegisterBot wires the use case.
func NewRegisterBot(deps Dependencies) RegisterBot {
	return RegisterBot{deps: deps, rec: recorder{deps: deps}}
}

// Execute registers the bot. A bot with the same id is a ConflictError.
func (uc RegisterBot) Execute(ctx context.Context, definition BotDefinition) (domain.Bot, error) {
	bot, err := buildBot(definition, uc.deps.Clock.Now())
	if err != nil {
		return domain.Bot{}, err
	}
	if _, err := uc.deps.Bots.Get(ctx, bot.ID()); err == nil {
		_ = uc.rec.audit(ctx, "bot.register", bot.ID().String(), domain.AuditDenied, map[string]string{"reason": "already exists"})
		return domain.Bot{}, domain.ConflictError{Kind: "bot", ID: bot.ID().String()}
	} else if !domain.IsNotFound(err) {
		return domain.Bot{}, err
	}
	if err := bot.Register(uc.deps.Clock.Now()); err != nil {
		return domain.Bot{}, err
	}
	if err := uc.deps.Bots.Save(ctx, bot); err != nil {
		return domain.Bot{}, err
	}
	if err := uc.rec.event(ctx, domain.EventBotRegistered, bot, "", "", map[string]any{
		"name": bot.Name().String(), "owner": bot.Owner().String(), "allowedTools": bot.AllowedTools().Names(),
	}); err != nil {
		return domain.Bot{}, err
	}
	if err := uc.rec.audit(ctx, "bot.register", bot.ID().String(), domain.AuditOK, nil); err != nil {
		return domain.Bot{}, err
	}
	return bot, nil
}

func buildBot(d BotDefinition, now time.Time) (domain.Bot, error) {
	rawID := d.ID
	if rawID == "" {
		rawID = d.Name
	}
	id, err := domain.NewBotID(rawID)
	if err != nil {
		return domain.Bot{}, err
	}
	tenant, err := domain.NewTenantID(d.Tenant)
	if err != nil {
		return domain.Bot{}, err
	}
	name, err := domain.NewBotName(d.Name)
	if err != nil {
		return domain.Bot{}, err
	}
	owner, err := domain.NewOwner(d.Owner)
	if err != nil {
		return domain.Bot{}, err
	}
	mission, err := domain.NewMission(d.Mission)
	if err != nil {
		return domain.Bot{}, err
	}
	tools, err := domain.NewAllowedTools(d.AllowedTools)
	if err != nil {
		return domain.Bot{}, err
	}
	model, err := domain.NewModelPolicy(d.Model.Model, d.Model.RoutingPolicy, d.Model.AllowRemoteProviders, d.Model.MaxContextTokens)
	if err != nil {
		return domain.Bot{}, err
	}
	budgets, err := domain.NewBudgets(d.Budgets.MaxModelCallsPerTask, d.Budgets.MaxToolCallsPerTask, d.Budgets.MaxDelegationDepth, time.Duration(d.Budgets.TaskTimeoutSeconds)*time.Second)
	if err != nil {
		return domain.Bot{}, err
	}
	bot, err := domain.NewBot(id, tenant, name, owner, mission, tools, model, budgets, now)
	if err != nil {
		return domain.Bot{}, err
	}
	level, err := domain.ParseAutonomyLevel(d.Level)
	if err != nil {
		return domain.Bot{}, err
	}
	rules, err := domain.RestoreToolRules(d.ToolRules, tools)
	if err != nil {
		return domain.Bot{}, err
	}
	if err := bot.Govern(level, rules); err != nil {
		return domain.Bot{}, err
	}
	kinds := make([]string, 0, len(d.Triggers))
	for _, t := range d.Triggers {
		kinds = append(kinds, t.Type)
	}
	triggers, err := domain.NewTriggers(kinds)
	if err != nil {
		return domain.Bot{}, err
	}
	placement, err := domain.NewPlacement(d.Placement.Nodes, d.Placement.Selector)
	if err != nil {
		return domain.Bot{}, err
	}
	bot.Wire(triggers, placement)
	return bot, nil
}
