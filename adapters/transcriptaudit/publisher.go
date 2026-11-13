package transcriptaudit

import (
	"context"

	"github.com/underpass-ai/axlr-bots/application"
)

// Publisher is the port the adapter publishes through: the operator API's
// publish route (unixapi.Client over the control plane's socket).
type Publisher interface {
	PublishGroupEvent(ctx context.Context, command application.PublishGroupEventCommand) (application.PublishAnswer, error)
}
