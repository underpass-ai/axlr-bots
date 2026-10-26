package application

import (
	"context"

	"github.com/underpass-ai/axlr-bots/domain"
)

// GetGroup reads one team: its members, in the tenant given or, without
// one, in the only tenant that has a team of that name.
type GetGroup struct {
	deps Dependencies
}

// NewGetGroup wires the use case.
func NewGetGroup(deps Dependencies) GetGroup { return GetGroup{deps: deps} }

// Execute returns the group, a domain.NotFoundError when no bot names it,
// or a domain.ValidationError when several tenants have it and none is
// given.
func (uc GetGroup) Execute(ctx context.Context, name, tenant string) (Group, error) {
	team, err := domain.NewTeamName(name)
	if err != nil {
		return Group{}, err
	}
	if tenant != "" {
		if _, err := domain.NewTenantID(tenant); err != nil {
			return Group{}, err
		}
	}
	groups, err := NewListGroups(uc.deps).Execute(ctx)
	if err != nil {
		return Group{}, err
	}
	return teamOf(groups, team, tenant)
}
