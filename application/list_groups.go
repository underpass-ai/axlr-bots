package application

import (
	"cmp"
	"context"
	"slices"
	"strings"

	"github.com/underpass-ai/axlr-bots/domain"
)

// ListGroups reads the teams from the bots: one group per tenant and team
// name a bot names, with every bot that names it, whatever its status.
type ListGroups struct {
	deps Dependencies
}

// NewListGroups wires the use case.
func NewListGroups(deps Dependencies) ListGroups { return ListGroups{deps: deps} }

// Execute returns the groups sorted by tenant and name, each with its
// members sorted by id.
func (uc ListGroups) Execute(ctx context.Context) ([]Group, error) {
	bots, err := uc.deps.Bots.List(ctx)
	if err != nil {
		return nil, err
	}
	type key struct{ tenant, name string }
	byKey := map[key]*Group{}
	for _, bot := range bots {
		for _, team := range bot.Teams().Names() {
			k := key{bot.TenantID().String(), team.String()}
			group, ok := byKey[k]
			if !ok {
				group = &Group{TenantID: k.tenant, Name: k.name}
				byKey[k] = group
			}
			group.Members = append(group.Members, memberOf(bot))
		}
	}
	groups := make([]Group, 0, len(byKey))
	for _, group := range byKey {
		slices.SortFunc(group.Members, func(a, b GroupMember) int { return strings.Compare(a.BotID, b.BotID) })
		groups = append(groups, *group)
	}
	slices.SortFunc(groups, func(a, b Group) int {
		return cmp.Or(strings.Compare(a.TenantID, b.TenantID), strings.Compare(a.Name, b.Name))
	})
	return groups, nil
}

// teamOf finds one group by name, in the tenant given or, without one,
// in the only tenant that has it.
func teamOf(groups []Group, name domain.TeamName, tenant string) (Group, error) {
	var found []Group
	for _, group := range groups {
		if group.Name == name.String() && (tenant == "" || group.TenantID == tenant) {
			found = append(found, group)
		}
	}
	switch len(found) {
	case 0:
		return Group{}, domain.NotFoundError{Kind: "group", ID: name.String()}
	case 1:
		return found[0], nil
	}
	tenants := make([]string, len(found))
	for i, group := range found {
		tenants[i] = group.TenantID
	}
	return Group{}, domain.ValidationError{Field: "tenant", Reason: "group " + name.String() + " exists in tenants " + strings.Join(tenants, ", ") + "; name one"}
}
