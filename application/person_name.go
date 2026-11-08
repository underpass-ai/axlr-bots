package application

import (
	"strings"

	"github.com/underpass-ai/axlr-bots/domain"
)

// personName reports a validated actor name that may be a person's: no
// policy name and, on a daemon, not the control plane's own.
func personName(deps Dependencies, name string) bool {
	if strings.HasPrefix(name, domain.PolicyActorPrefix) {
		return false
	}
	return !deps.Daemon || domain.ActorKey(name) != domain.ActorKey(deps.Actor)
}
