package application

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"

	"github.com/underpass-ai/axlr-bots/domain"
)

// UploadTranscript keeps the whole transcript of a session a node's
// execution ran, before the node reports: only from the node that holds
// the execution, while it runs, and only when the bytes hash to the
// SHA-256 the node names.
type UploadTranscript struct {
	deps Dependencies
	rec  recorder
}

// NewUploadTranscript wires the use case.
func NewUploadTranscript(deps Dependencies) UploadTranscript {
	return UploadTranscript{deps: deps, rec: recorder{deps: deps}}
}

// Execute keeps the transcript and answers its record.
func (uc UploadTranscript) Execute(ctx context.Context, node domain.NodeID, id domain.ExecutionID, upload TranscriptUpload) (TranscriptRecord, error) {
	if uc.deps.Transcripts == nil {
		return TranscriptRecord{}, domain.StateError{Subject: "control plane", Current: "keeps no transcripts", Attempted: "take a transcript"}
	}
	if err := validSession(upload.Session); err != nil {
		return TranscriptRecord{}, err
	}
	if len(upload.Content) > MaxTranscriptBytes {
		return TranscriptRecord{}, uc.refuse(ctx, id, upload, domain.ValidationError{Field: "transcript", Reason: "is at most 32 MiB"})
	}
	sum := sha256.Sum256(upload.Content)
	if !strings.EqualFold(hex.EncodeToString(sum[:]), upload.SHA256) {
		return TranscriptRecord{}, uc.refuse(ctx, id, upload, domain.ValidationError{Field: "transcript", Reason: "does not hash to the SHA-256 named"})
	}
	execution, err := uc.deps.Executions.Get(ctx, id)
	if err != nil {
		return TranscriptRecord{}, err
	}
	if execution.NodeID() != node {
		return TranscriptRecord{}, uc.refuse(ctx, id, upload, domain.StateError{Subject: "execution " + id.String(), Current: "assigned to " + orNobody(execution.NodeID()), Attempted: "take a transcript from node " + node.String()})
	}
	if execution.Outcome() != domain.ExecutionRunning {
		return TranscriptRecord{}, uc.refuse(ctx, id, upload, domain.StateError{Subject: "execution " + id.String(), Current: execution.Outcome().String(), Attempted: "take a transcript"})
	}
	task, err := uc.deps.Tasks.Get(ctx, execution.TaskID())
	if err != nil {
		return TranscriptRecord{}, err
	}
	record := TranscriptRecord{Execution: id, Task: task.ID(), Bot: task.BotID(), Session: upload.Session, Size: int64(len(upload.Content)), SHA256: hex.EncodeToString(sum[:]), ReceivedAt: uc.deps.Clock.Now().UTC()}
	content := upload.Content
	if uc.deps.TranscriptFilter != nil {
		filtered, err := uc.deps.TranscriptFilter.FilterTranscript(ctx, record, content)
		if err != nil {
			return TranscriptRecord{}, uc.refuse(ctx, id, upload, err)
		}
		kept := sha256.Sum256(filtered)
		if hex.EncodeToString(kept[:]) != record.SHA256 {
			record.Filtered, record.SentSHA256 = true, record.SHA256
			record.Size, record.SHA256 = int64(len(filtered)), hex.EncodeToString(kept[:])
		}
		content = filtered
	}
	kept, err := uc.deps.Transcripts.SaveTranscript(ctx, record, content)
	if err != nil {
		return TranscriptRecord{}, err
	}
	details := map[string]string{"session": upload.Session, "node": node.String(), "size": strconv.FormatInt(record.Size, 10), "sha256": record.SHA256}
	if record.Filtered {
		details["filtered"], details["sent_sha256"] = "true", record.SentSHA256
	}
	if err := uc.rec.audit(ctx, "transcript.upload", id.String(), domain.AuditOK, details); err != nil {
		return TranscriptRecord{}, err
	}
	return kept, nil
}

func (uc UploadTranscript) refuse(ctx context.Context, id domain.ExecutionID, upload TranscriptUpload, cause error) error {
	_ = uc.rec.audit(ctx, "transcript.upload", id.String(), domain.AuditDenied, map[string]string{"session": upload.Session, "reason": cause.Error()})
	return cause
}
