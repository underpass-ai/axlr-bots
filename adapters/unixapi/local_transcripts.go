package unixapi

import (
	"context"

	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

// ListTranscripts lists the transcripts kept after the cursor.
func (l *Local) ListTranscripts(ctx context.Context, after int64, limit int) ([]application.TranscriptRecord, error) {
	return application.NewListTranscripts(l.deps).Execute(ctx, after, limit)
}

// GetTranscript reads one transcript whole.
func (l *Local) GetTranscript(ctx context.Context, execution, session string) ([]byte, error) {
	executionID, err := domain.NewExecutionID(execution)
	if err != nil {
		return nil, err
	}
	_, content, err := application.NewGetTranscript(l.deps).Execute(ctx, executionID, session)
	return content, err
}
