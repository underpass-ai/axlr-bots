package nats

import (
	"context"

	"github.com/underpass-ai/axlr-bots/application"
)

// PublicationService is what the control plane's consumer of the
// publications drives (docs/architecture/topics-on-nats.md): the teams,
// to find the one a subject names; the operator's publication on a team,
// under the same rules as POST /v1/groups/{name}/events; and the audit
// of one it refused.
type PublicationService interface {
	ListGroups(ctx context.Context) ([]application.Group, error)
	PublishGroupEvent(ctx context.Context, command application.PublishGroupEventCommand) (application.PublishAnswer, error)
	RefusePublication(ctx context.Context, refused application.RefusedPublication) error
}
