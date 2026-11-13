package application

import (
	"context"

	"github.com/underpass-ai/axlr-bots/domain"
)

// ListTranscripts lists the transcripts the control plane keeps, oldest
// first, after a cursor: what an auditor reads to find the ones it has
// not read yet.
type ListTranscripts struct {
	deps Dependencies
}

// NewListTranscripts wires the use case.
func NewListTranscripts(deps Dependencies) ListTranscripts {
	return ListTranscripts{deps: deps}
}

// Execute lists at most limit records after the cursor (limit 0 or over
// the bound: MaxTranscriptList).
func (uc ListTranscripts) Execute(ctx context.Context, after int64, limit int) ([]TranscriptRecord, error) {
	if uc.deps.Transcripts == nil {
		return nil, domain.StateError{Subject: "control plane", Current: "keeps no transcripts", Attempted: "list transcripts"}
	}
	if after < 0 {
		return nil, domain.ValidationError{Field: "after", Reason: "is a cursor of 0 or more"}
	}
	if limit <= 0 || limit > MaxTranscriptList {
		limit = MaxTranscriptList
	}
	records, err := uc.deps.Transcripts.ListTranscripts(ctx, after, limit)
	if records == nil && err == nil {
		records = []TranscriptRecord{}
	}
	return records, err
}
