package application

import "github.com/underpass-ai/axlr-bots/domain"

// AskToolName is the tool the node serves to an execution whose bot may
// ask other teams; the control plane adds it to the execution's allowed
// tools, so the policy names it like any other.
const AskToolName = "axlr_bots_ask_team"

// AskOffer tells the node that the execution's bot may ask other teams:
// the teams it may ask (the tool's enum). The control plane checks every
// call again; the offer only describes the tool.
type AskOffer struct {
	Teams []string
}

// askOffer is the bot's offer, or nil when it asks no team or the task
// answers another team's question: one hop between teams, no further.
func askOffer(bot domain.Bot, task domain.Task) *AskOffer {
	if !bot.Ask().Enabled() || task.Trigger().Type == domain.TriggerQuestion {
		return nil
	}
	return &AskOffer{Teams: teamNames(bot.Ask().Teams())}
}
