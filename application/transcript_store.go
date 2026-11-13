package application

import (
	"context"

	"github.com/underpass-ai/axlr-bots/domain"
)

// TranscriptStore keeps the whole transcript of each execution's runtime
// session, uncut, for observability and audit
// (docs/architecture/transcripts.md).
type TranscriptStore interface {
	// SaveTranscript keeps the content under the record's execution and
	// session, replacing one kept before for them (a later upload of a
	// session carries everything the earlier did), and answers the
	// record as kept, Seq included.
	SaveTranscript(ctx context.Context, record TranscriptRecord, content []byte) (TranscriptRecord, error)
	// LoadTranscript reads one; a domain.NotFoundError when there is none.
	LoadTranscript(ctx context.Context, execution domain.ExecutionID, session string) (TranscriptRecord, []byte, error)
	// ListTranscripts lists the records after the cursor, oldest first,
	// at most limit.
	ListTranscripts(ctx context.Context, after int64, limit int) ([]TranscriptRecord, error)
}
