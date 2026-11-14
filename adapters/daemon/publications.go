package daemon

import (
	"context"

	"github.com/underpass-ai/axlr-bots/adapters/unixapi"
	"github.com/underpass-ai/axlr-bots/application"
)

// publications is the service the topic bus drives: the teams and the
// operator's publication through the service the API serves, under the
// runner's lock, and the audit of a refused one.
type publications struct {
	*unixapi.Local
	refuse application.RefusePublication
}

func (p publications) RefusePublication(ctx context.Context, refused application.RefusedPublication) error {
	return p.refuse.Execute(ctx, refused)
}
