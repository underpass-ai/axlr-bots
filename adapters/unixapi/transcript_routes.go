package unixapi

import (
	"io"
	"net/http"
	"strconv"

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

// listTranscripts is GET /v1/transcripts?after=&limit=: the records of
// the transcripts kept after the cursor, oldest first.
func (s *Server) listTranscripts(w http.ResponseWriter, r *http.Request) {
	after, limit := int64(0), 0
	if raw := r.URL.Query().Get("after"); raw != "" {
		value, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			respond(w, 0, nil, domain.ValidationError{Field: "after", Reason: "is a number"})
			return
		}
		after = value
	}
	if raw := r.URL.Query().Get("limit"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil {
			respond(w, 0, nil, domain.ValidationError{Field: "limit", Reason: "is a number"})
			return
		}
		limit = value
	}
	records, err := application.NewListTranscripts(s.service.Dependencies()).Execute(r.Context(), after, limit)
	respond(w, http.StatusOK, records, err)
}

// getTranscript is GET /v1/executions/{id}/transcripts/{session}: the
// transcript's bytes, as the node sent them, with their SHA-256.
func (s *Server) getTranscript(w http.ResponseWriter, r *http.Request) {
	id, err := domain.NewExecutionID(r.PathValue("id"))
	if err != nil {
		respond(w, 0, nil, err)
		return
	}
	record, content, err := application.NewGetTranscript(s.service.Dependencies()).Execute(r.Context(), id, r.PathValue("session"))
	if err != nil {
		respond(w, 0, nil, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Length", strconv.Itoa(len(content)))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set(TranscriptSHA256Header, record.SHA256)
	writeDeadline(w, artifactWriteTimeout)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(content)
}
