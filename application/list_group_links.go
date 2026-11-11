package application

import (
	"context"
	"slices"
	"strings"

	"github.com/underpass-ai/axlr-bots/domain"
)

// ListGroupLinks derives a team's links in the team channel: the teams it
// may ask and the teams that may ask it. A link from A to B holds while
// some bot whose memory team is A names B in its ask.teams and some bot
// whose memory team is B answers A through its question trigger; one side
// alone is no link. It reads what the bots declare, not who is free: a
// link whose responders are all paused or offline still shows, and the
// ask is refused team_unavailable.
type ListGroupLinks struct {
	deps Dependencies
}

// NewListGroupLinks wires the use case.
func NewListGroupLinks(deps Dependencies) ListGroupLinks { return ListGroupLinks{deps: deps} }

// Execute answers the team's links, from the team and to it, sorted by
// the asking team and then the answering one.
func (uc ListGroupLinks) Execute(ctx context.Context, name, tenant string) ([]GroupLink, error) {
	group, err := NewGetGroup(uc.deps).Execute(ctx, name, tenant)
	if err != nil {
		return nil, err
	}
	all, err := uc.deps.Bots.List(ctx)
	if err != nil {
		return nil, err
	}
	var bots []domain.Bot
	for _, bot := range all {
		if string(bot.TenantID()) == group.TenantID {
			bots = append(bots, bot)
		}
	}
	slices.SortFunc(bots, func(a, b domain.Bot) int { return strings.Compare(a.ID().String(), b.ID().String()) })
	team := domain.TeamName(group.Name)
	links := map[[2]domain.TeamName]*GroupLink{}
	for _, asker := range bots {
		from := memoryTeamOf(asker)
		if from == "" {
			continue
		}
		for _, to := range asker.Ask().Teams() {
			if to == from || (from != team && to != team) {
				continue
			}
			responders := respondersOf(bots, to, from)
			if len(responders) == 0 {
				continue
			}
			key := [2]domain.TeamName{from, to}
			link := links[key]
			if link == nil {
				link = &GroupLink{From: from.String(), To: to.String(), Responders: responders}
				links[key] = link
			}
			link.Askers = append(link.Askers, asker.ID().String())
		}
	}
	out := make([]GroupLink, 0, len(links))
	for _, link := range links {
		out = append(out, *link)
	}
	slices.SortFunc(out, func(a, b GroupLink) int {
		if c := strings.Compare(a.From, b.From); c != 0 {
			return c
		}
		return strings.Compare(a.To, b.To)
	})
	return out, nil
}

// respondersOf are the ids of the bots whose memory team is team and that
// answer the asking team, in the order of bots.
func respondersOf(bots []domain.Bot, team, asking domain.TeamName) []string {
	var out []string
	for _, bot := range bots {
		if memoryTeamOf(bot) == team && bot.AnswersQuestionsFrom(asking) {
			out = append(out, bot.ID().String())
		}
	}
	return out
}
