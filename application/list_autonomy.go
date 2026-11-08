package application

import (
	"context"
	"sort"

	"github.com/underpass-ai/axlr-bots/domain"
)

// ListAutonomy lists the runbooks on ladders, by runbook and namespace.
type ListAutonomy struct {
	deps Dependencies
}

// NewListAutonomy wires the use case.
func NewListAutonomy(deps Dependencies) ListAutonomy { return ListAutonomy{deps: deps} }

// Execute answers the records the query names, with their thresholds.
func (uc ListAutonomy) Execute(ctx context.Context, query AutonomyQuery) ([]AutonomyView, error) {
	all, err := uc.deps.Autonomy.List(ctx)
	if err != nil {
		return nil, err
	}
	views := make([]AutonomyView, 0, len(all))
	for _, autonomy := range all {
		if !query.matches(autonomy) {
			continue
		}
		view, err := viewOf(ctx, uc.deps, autonomy)
		if err != nil {
			return nil, err
		}
		views = append(views, view)
	}
	sort.Slice(views, func(i, j int) bool {
		a, b := views[i].Autonomy, views[j].Autonomy
		if a.Runbook() != b.Runbook() {
			return a.Runbook() < b.Runbook()
		}
		return a.Namespace() < b.Namespace()
	})
	return views, nil
}

func (q AutonomyQuery) matches(a domain.RunbookAutonomy) bool {
	return (q.Tenant == "" || q.Tenant == a.TenantID().String()) && (q.Bot == "" || q.Bot == a.BotID().String()) &&
		(q.Runbook == "" || q.Runbook == a.Runbook().String()) && (q.Namespace == "" || q.Namespace == a.Namespace())
}

// viewOf is the record with the thresholds its bot's ladder has now.
func viewOf(ctx context.Context, deps Dependencies, autonomy domain.RunbookAutonomy) (AutonomyView, error) {
	view := AutonomyView{Autonomy: autonomy, Saved: true}
	bot, err := deps.Bots.Get(ctx, autonomy.BotID())
	if domain.IsNotFound(err) {
		return view, nil
	}
	if err != nil {
		return AutonomyView{}, err
	}
	if trigger, ok := bot.Triggers().Incident(domain.IncidentPhaseApply); ok {
		if ladder, ok := trigger.Ladder(); ok {
			thresholds := ladder.Thresholds().Snapshot()
			view.Thresholds = &thresholds
		}
	}
	return view, nil
}
