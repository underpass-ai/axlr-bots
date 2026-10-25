package application

import (
	"io"

	"github.com/underpass-ai/axlr-bots/domain"
)

// Workspace opens what an execution delivered, in the workspace of the
// bot on the host that ran it: the runtime adapter resolves the
// reference inside the workspace and refuses one that leaves it. The
// node agent reads it to upload; the control plane reads it for the
// executions it runs in process.
type Workspace interface {
	OpenDelivered(bot domain.BotID, reference string) (io.ReadCloser, error)
}
