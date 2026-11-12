package application

import (
	"context"
	"slices"
	"time"

	"github.com/underpass-ai/axlr-bots/domain"
)

// UpdateBot applies a patch to a bot's definition: its mission, level,
// tools and rules, triggers, placement, approvals (or only the calls
// they approve without a person), delegation, teams, model or budgets.
// The status is not a field: EnableBot changes it. The bot is validated
// as a whole after the patch, so a patch that strands a tool rule or
// names a bad trigger changes nothing.
type UpdateBot struct {
	deps Dependencies
	rec  recorder
}

// NewUpdateBot wires the use case.
func NewUpdateBot(deps Dependencies) UpdateBot {
	return UpdateBot{deps: deps, rec: recorder{deps: deps}}
}

// Execute applies the patch and returns the bot.
func (uc UpdateBot) Execute(ctx context.Context, id domain.BotID, patch BotPatch) (domain.Bot, error) {
	bot, err := uc.deps.Bots.Get(ctx, id)
	if err != nil {
		return domain.Bot{}, err
	}
	return uc.update(ctx, bot, patch, nil, nil)
}

// update applies the patch to the bot as read, runs settle on it (when
// given) in the same save, and publishes and audits the change as every
// update does, with note added to the event's payload: applying a
// staged change is this update, with the staged change dropped by
// settle and named in note.
func (uc UpdateBot) update(ctx context.Context, bot domain.Bot, patch BotPatch, settle func(*domain.Bot), note map[string]any) (domain.Bot, error) {
	id := bot.ID()
	previous, previousVersion := bot.Level(), bot.Version()
	changed, err := applyPatch(&bot, patch)
	if err != nil {
		_ = uc.rec.audit(ctx, "bot.update", id.String(), domain.AuditDenied, map[string]string{"reason": err.Error()})
		return domain.Bot{}, err
	}
	if len(changed) == 0 {
		return domain.Bot{}, errEmptyPatch
	}
	if err := checkChanged(ctx, uc.deps, bot, changed); err != nil {
		_ = uc.rec.audit(ctx, "bot.update", id.String(), domain.AuditDenied, map[string]string{"reason": err.Error()})
		return domain.Bot{}, err
	}
	bot.Touch(uc.deps.Clock.Now())
	if settle != nil {
		settle(&bot)
	}
	if err := uc.deps.Bots.Save(ctx, bot); err != nil {
		return domain.Bot{}, err
	}
	payload := map[string]any{"fields": changed}
	for key, value := range note {
		payload[key] = value
	}
	if bot.Level() != previous {
		// A turned dial says where from and where to, for the timeline.
		payload["previousLevel"], payload["level"] = previous.String(), bot.Level().String()
	}
	if bot.Version() != previousVersion {
		payload["previousVersion"], payload["version"] = previousVersion.String(), bot.Version().String()
	}
	if err := uc.rec.event(ctx, domain.EventBotUpdated, bot, "", "", payload); err != nil {
		return domain.Bot{}, err
	}
	if slices.Contains(changed, "subscriptions") {
		list := bot.Subscriptions().Snapshot()
		if list == nil {
			list = []domain.SubscriptionSnapshot{}
		}
		if err := uc.rec.event(ctx, domain.EventSubscriptionChanged, bot, "", "", map[string]any{"subscriptions": list, "by": domain.FromOperator}); err != nil {
			return domain.Bot{}, err
		}
	}
	if err := uc.rec.audit(ctx, "bot.update", id.String(), domain.AuditOK, map[string]string{"fields": joinStrings(changed)}); err != nil {
		return domain.Bot{}, err
	}
	return bot, nil
}

// errEmptyPatch refuses a patch that names no field.
var errEmptyPatch = domain.ValidationError{Field: "patch", Reason: "names no field to change"}

// checkPatch applies the patch to the bot in memory and checks it as
// an update does before it saves, and names the fields changed. Staging
// runs it on a copy, so a staged change is one the update takes now.
func checkPatch(ctx context.Context, deps Dependencies, bot *domain.Bot, patch BotPatch) ([]string, error) {
	changed, err := applyPatch(bot, patch)
	if err != nil {
		return nil, err
	}
	if len(changed) == 0 {
		return nil, errEmptyPatch
	}
	if err := checkChanged(ctx, deps, *bot, changed); err != nil {
		return nil, err
	}
	return changed, nil
}

// checkChanged is what an update checks beyond the bot itself: a
// subscription to senders the bot may not hear, a ladder whose
// approvers do not fit.
func checkChanged(ctx context.Context, deps Dependencies, bot domain.Bot, changed []string) error {
	if slices.Contains(changed, "subscriptions") {
		if err := checkSubscriptionSenders(ctx, deps, bot); err != nil {
			return err
		}
	}
	return checkLadderApprovers(ctx, deps, bot)
}

// applyPatch changes the bot in memory and names the fields it changed.
func applyPatch(bot *domain.Bot, patch BotPatch) ([]string, error) {
	var changed []string
	mission, tools, model, budgets := bot.Mission(), bot.AllowedTools(), bot.ModelPolicy(), bot.Budgets()
	level, rules := bot.Level(), bot.ToolRules()
	var err error
	if patch.Mission != nil {
		if mission, err = domain.NewMission(*patch.Mission); err != nil {
			return nil, err
		}
		changed = append(changed, "mission")
	}
	if patch.AllowedTools != nil {
		if tools, err = domain.NewAllowedTools(*patch.AllowedTools); err != nil {
			return nil, err
		}
		changed = append(changed, "allowedTools")
	}
	if patch.Model != nil {
		if model, err = domain.NewModelPolicy(patch.Model.Model, patch.Model.RoutingPolicy, patch.Model.AllowRemoteProviders, patch.Model.MaxContextTokens); err != nil {
			return nil, err
		}
		changed = append(changed, "model")
	}
	if patch.Budgets != nil {
		if budgets, err = domain.NewBudgets(patch.Budgets.MaxModelCallsPerTask, patch.Budgets.MaxToolCallsPerTask, patch.Budgets.MaxDelegationDepth, time.Duration(patch.Budgets.TaskTimeoutSeconds)*time.Second); err != nil {
			return nil, err
		}
		changed = append(changed, "budgets")
	}
	if patch.Level != nil {
		if level, err = domain.ParseAutonomyLevel(*patch.Level); err != nil {
			return nil, err
		}
		changed = append(changed, "level")
	}
	if patch.ToolRules != nil {
		if rules, err = domain.RestoreToolRules(*patch.ToolRules, tools); err != nil {
			return nil, err
		}
		changed = append(changed, "toolRules")
	}
	if patch.AllowedTools != nil || patch.ToolRules != nil || patch.Mission != nil || patch.Model != nil || patch.Budgets != nil || patch.Level != nil {
		// The rules are cleared while the tools change, then governed
		// again against the new list: a narrower list may not strand a rule.
		if err := bot.Govern(bot.Level(), domain.ToolRules{}); err != nil {
			return nil, err
		}
		if err := bot.Redefine(mission, tools, model, budgets); err != nil {
			return nil, err
		}
		if err := bot.Govern(level, rules); err != nil {
			return nil, err
		}
	}
	subscriptions := bot.Subscriptions().Snapshot()
	subscribed := patch.Subscriptions != nil
	if subscribed {
		subscriptions = *patch.Subscriptions
	}
	if patch.Triggers != nil || patch.Placement != nil {
		triggers, placement := bot.Triggers(), bot.Placement()
		if patch.Triggers != nil {
			// Event triggers are read as subscriptions, for one release.
			var plain []domain.TriggerSnapshot
			before := len(subscriptions)
			plain, subscriptions = domain.SubscriptionsFromTriggers(*patch.Triggers, subscriptions)
			subscribed = subscribed || len(subscriptions) != before
			if triggers, err = domain.NewTriggers(plain); err != nil {
				return nil, err
			}
			changed = append(changed, "triggers")
		}
		if patch.Placement != nil {
			if placement, err = domain.NewPlacement(patch.Placement.Nodes, patch.Placement.Selector); err != nil {
				return nil, err
			}
			changed = append(changed, "placement")
		}
		bot.Wire(triggers, placement)
	}
	if patch.Approvals != nil {
		approvals, err := domain.NewApprovals(*patch.Approvals)
		if err != nil {
			return nil, err
		}
		bot.SetApprovals(approvals)
		changed = append(changed, "approvals")
	}
	if patch.AutoApprove != nil {
		approvals, err := bot.Approvals().WithAutoApprove(*patch.AutoApprove)
		if err != nil {
			return nil, err
		}
		bot.SetApprovals(approvals)
		changed = append(changed, "autoApprove")
	}
	if patch.Delegation != nil {
		delegation, err := domain.NewDelegation(*patch.Delegation)
		if err != nil {
			return nil, err
		}
		if err := bot.SetDelegation(delegation); err != nil {
			return nil, err
		}
		changed = append(changed, "delegation")
	}
	if patch.Ask != nil {
		ask, err := domain.NewAskPolicy(*patch.Ask)
		if err != nil {
			return nil, err
		}
		bot.SetAsk(ask)
		changed = append(changed, "ask")
	}
	if patch.Groups != nil {
		teams, err := domain.NewTeams(*patch.Groups)
		if err != nil {
			return nil, err
		}
		bot.JoinTeams(teams)
		changed = append(changed, "groups")
	}
	if patch.Events != nil {
		events, err := domain.NewEventPolicy(*patch.Events)
		if err != nil {
			return nil, err
		}
		bot.SetEvents(events)
		changed = append(changed, "events")
	}
	if patch.Runtime != nil {
		runtime, err := domain.NewRuntime(patch.Runtime)
		if err != nil {
			return nil, err
		}
		if err := bot.SetRuntime(runtime); err != nil {
			return nil, err
		}
		changed = append(changed, "runtime")
	}
	if patch.Skills != nil {
		skills, err := domain.NewSkillPins(*patch.Skills)
		if err != nil {
			return nil, err
		}
		bot.SetSkills(skills)
		changed = append(changed, "skills")
	}
	if patch.Version != nil {
		// An empty version clears it: the bot names none.
		var version domain.SemanticVersion
		if *patch.Version != "" {
			if version, err = domain.ParseSemanticVersion("version", *patch.Version); err != nil {
				return nil, err
			}
		}
		bot.SetVersion(version)
		changed = append(changed, "version")
	}
	// Narrower tools or a lower level may no longer carry a Jev program,
	// and a Jev program reads no skill.
	if err := bot.CheckRuntime(); err != nil {
		return nil, err
	}
	// Nor an incident trigger's narrower policy for its phase.
	if err := bot.CheckTriggers(); err != nil {
		return nil, err
	}
	// New teams may hold a team the bot asks.
	if err := bot.CheckAsk(); err != nil {
		return nil, err
	}
	if subscribed {
		subs, err := domain.NewSubscriptions(subscriptions)
		if err != nil {
			return nil, err
		}
		if err := keepsPendingAsks(bot.Subscriptions(), subs); err != nil {
			return nil, err
		}
		bot.SetSubscriptions(subs)
		changed = append(changed, "subscriptions")
	}
	return changed, nil
}

func joinStrings(items []string) string {
	joined := ""
	for i, item := range items {
		if i > 0 {
			joined += ","
		}
		joined += item
	}
	return joined
}
