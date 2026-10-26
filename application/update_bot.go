package application

import (
	"context"
	"time"

	"github.com/underpass-ai/axlr-bots/domain"
)

// UpdateBot applies a patch to a bot's definition: its mission, level,
// tools and rules, triggers, placement, approvals, delegation, teams,
// model or budgets.
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
	changed, err := applyPatch(&bot, patch)
	if err != nil {
		_ = uc.rec.audit(ctx, "bot.update", id.String(), domain.AuditDenied, map[string]string{"reason": err.Error()})
		return domain.Bot{}, err
	}
	if len(changed) == 0 {
		return domain.Bot{}, domain.ValidationError{Field: "patch", Reason: "names no field to change"}
	}
	bot.Touch(uc.deps.Clock.Now())
	if err := uc.deps.Bots.Save(ctx, bot); err != nil {
		return domain.Bot{}, err
	}
	if err := uc.rec.event(ctx, domain.EventBotUpdated, bot, "", "", map[string]any{"fields": changed}); err != nil {
		return domain.Bot{}, err
	}
	if err := uc.rec.audit(ctx, "bot.update", id.String(), domain.AuditOK, map[string]string{"fields": joinStrings(changed)}); err != nil {
		return domain.Bot{}, err
	}
	return bot, nil
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
	if patch.Triggers != nil || patch.Placement != nil {
		triggers, placement := bot.Triggers(), bot.Placement()
		if patch.Triggers != nil {
			if triggers, err = domain.NewTriggers(*patch.Triggers); err != nil {
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
	if patch.Groups != nil {
		teams, err := domain.NewTeams(*patch.Groups)
		if err != nil {
			return nil, err
		}
		bot.JoinTeams(teams)
		changed = append(changed, "groups")
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
