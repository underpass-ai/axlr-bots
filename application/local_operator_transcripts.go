package application

import (
	"context"

	"github.com/underpass-ai/axlr-bots/domain"
)

// ListTranscripts lists the transcripts kept after the cursor.
func (l *LocalOperator) ListTranscripts(ctx context.Context, after int64, limit int) ([]TranscriptRecord, error) {
	return NewListTranscripts(l.deps).Execute(ctx, after, limit)
}

// GetTranscript reads one transcript whole.
func (l *LocalOperator) GetTranscript(ctx context.Context, execution, session string) ([]byte, error) {
	executionID, err := domain.NewExecutionID(execution)
	if err != nil {
		return nil, err
	}
	_, content, err := NewGetTranscript(l.deps).Execute(ctx, executionID, session)
	return content, err
}
