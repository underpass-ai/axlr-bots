package transcriptaudit

import (
	"context"

	"github.com/underpass-ai/axlr-bots/application"
)

// Source is the port the adapter reads through: the operator API over
// the control plane's socket (unixapi.Client). It never reads the state
// directory.
type Source interface {
	ListGroups(ctx context.Context) ([]application.Group, error)
	ListTranscripts(ctx context.Context, after int64, limit int) ([]application.TranscriptRecord, error)
	GetTranscript(ctx context.Context, execution, session string) ([]byte, error)
}
