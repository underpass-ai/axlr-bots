package application

import "context"

// TranscriptFilter is the optional step between what a node sends and
// what the control plane keeps: it answers the bytes to keep (a
// redaction of secrets, say). The control plane wires none by default:
// a transcript is then kept exactly as the node sent it
// (docs/architecture/transcripts.md).
type TranscriptFilter interface {
	FilterTranscript(ctx context.Context, record TranscriptRecord, content []byte) ([]byte, error)
}
