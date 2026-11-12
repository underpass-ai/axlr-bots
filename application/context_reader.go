package application

import (
	"context"
	"errors"

	"github.com/underpass-ai/axlr-bots/domain"
)

// ErrContextNotFound says a team's store holds nothing under the key.
var ErrContextNotFound = errors.New("the store holds nothing under the key")

// ContextReader reads, without a model, what a team's store on the node
// holds under a KMP key, newest first, for the run of a task that carries
// the key (docs/architecture/team-subscriptions.md, "Done is a contract",
// E8d). A key the store does not have is ErrContextNotFound.
type ContextReader interface {
	ReadContext(ctx context.Context, team domain.TeamName, key domain.ContextKey) ([]string, error)
}

// contextRefsOf are the KMP keys the task's run is given what is under:
// the key its redelivery carries, in its own team's store, and the key
// another team's domain event named, in that team's. Invalid or repeated
// keys are left out.
func contextRefsOf(task domain.Task) []domain.ContextRef {
	var refs []domain.ContextRef
	add := func(key, team string) {
		ref := domain.ContextRef{Key: key, Team: team}
		if key == "" || ref.Validate() != nil {
			return
		}
		for _, known := range refs {
			if known == ref {
				return
			}
		}
		refs = append(refs, ref)
	}
	if redelivery, ok := redeliveryOf(task); ok {
		add(redelivery.Context, redelivery.ContextTeam)
	}
	if origin, ok := task.Origin(); ok {
		add(origin.Context, origin.Team)
	}
	return refs
}
