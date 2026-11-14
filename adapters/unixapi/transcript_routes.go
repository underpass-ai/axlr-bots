package unixapi

import (
	"io"
	"net/http"

	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

// TranscriptSHA256Header carries a transcript's SHA-256 in hex: the node
// names it on upload, the control plane answers it on read.
const TranscriptSHA256Header = "X-Axlr-Transcript-Sha256"

// uploadTranscript is PUT /v1/nodes/{id}/executions/{exec}/transcripts/{session}:
// the body is the session's bytes, whole; the control plane keeps them
// only when they hash to the SHA-256 header.
func (s *Server) uploadTranscript(w http.ResponseWriter, r *http.Request) {
	id, err := domain.NewNodeID(r.PathValue("id"))
	if err != nil {
		respond(w, 0, nil, err)
		return
	}
	executionID, err := domain.NewExecutionID(r.PathValue("exec"))
	if err != nil {
		respond(w, 0, nil, err)
		return
	}
	if r.ContentLength > application.MaxTranscriptBytes {
		respond(w, 0, nil, &http.MaxBytesError{Limit: application.MaxTranscriptBytes})
		return
	}
	content, err := io.ReadAll(http.MaxBytesReader(w, r.Body, application.MaxTranscriptBytes))
	if err != nil {
		respond(w, 0, nil, err)
		return
	}
	upload := application.TranscriptUpload{Session: r.PathValue("session"), Content: content, SHA256: r.Header.Get(TranscriptSHA256Header)}
	record, err := application.NewUploadTranscript(s.service.Dependencies()).Execute(r.Context(), id, executionID, upload)
	respond(w, http.StatusCreated, record, err)
}
