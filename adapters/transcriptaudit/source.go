package transcriptaudit

import (
	"context"

	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

// Source is the port the adapter reads through: the operator API over
// the control plane's socket (unixapi.Client). It never reads the state
// directory.
type Source interface {
	ListGroups(ctx context.Context) ([]application.Group, error)
	ListTranscripts(ctx context.Context, after int64, limit int) ([]application.TranscriptRecord, error)
	GetTranscript(ctx context.Context, execution, session string) ([]byte, error)
	// GetTask reads a transcript's task, for what started it: a task
	// started by the adapter's own event is not audited (no loop).
	GetTask(ctx context.Context, id string) (domain.TaskSnapshot, error)
}
