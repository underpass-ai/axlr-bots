package application

import (
	"context"

	"github.com/underpass-ai/axlr-bots/domain"
)

// GetTranscript reads one transcript the control plane keeps, whole and
// byte for byte as the node sent it.
type GetTranscript struct {
	deps Dependencies
}

// NewGetTranscript wires the use case.
func NewGetTranscript(deps Dependencies) GetTranscript {
	return GetTranscript{deps: deps}
}

// Execute answers the record and the bytes.
func (uc GetTranscript) Execute(ctx context.Context, id domain.ExecutionID, session string) (TranscriptRecord, []byte, error) {
	if uc.deps.Transcripts == nil {
		return TranscriptRecord{}, nil, domain.StateError{Subject: "control plane", Current: "keeps no transcripts", Attempted: "read a transcript"}
	}
	if err := validSession(session); err != nil {
		return TranscriptRecord{}, nil, err
	}
	return uc.deps.Transcripts.LoadTranscript(ctx, id, session)
}
