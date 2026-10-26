package application

import "github.com/underpass-ai/axlr-bots/domain"

// GroupMember is one bot of a team as the Teams view shows it: what its
// teammates read of it in their directory (its purpose, tools and where
// it runs), its status, every team it belongs to and whether it takes
// the tasks its teammates delegate.
type GroupMember struct {
	BotID           string                   `json:"botId"`
	Name            string                   `json:"name"`
	Status          string                   `json:"status"`
	Purpose         string                   `json:"purpose"`
	Tools           []string                 `json:"tools"`
	Where           string                   `json:"where"`
	Placement       domain.PlacementSnapshot `json:"placement"`
	Groups          []string                 `json:"groups"`
	TakesDelegation bool                     `json:"takesDelegation"`
}

// memberOf describes the bot as a member of its teams.
func memberOf(bot domain.Bot) GroupMember {
	return GroupMember{
		BotID: bot.ID().String(), Name: bot.Name().String(), Status: bot.Status().String(), Purpose: bot.Mission().Purpose(),
		Tools: bot.AllowedTools().Names(), Where: bot.Placement().Where(), Placement: bot.Placement().Snapshot(),
		Groups: bot.Teams().Snapshot(), TakesDelegation: bot.AcceptsTrigger(domain.TriggerDelegation) || bot.Delegation().FromTeam(),
	}
}
