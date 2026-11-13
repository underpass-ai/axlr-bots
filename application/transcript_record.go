package application

import (
	"time"

	"github.com/underpass-ai/axlr-bots/domain"
)

// MaxTranscriptBytes bounds one transcript a node uploads: a runtime
// session, every message of it, as the runtime keeps it. The largest
// AXLR session seen on gx10 on 13 November 2026 was 1.5 MiB.
const MaxTranscriptBytes = 32 << 20

// MaxTranscriptList bounds one page of ListTranscripts.
const MaxTranscriptList = 500

// TranscriptRecord is what the control plane keeps beside one
// transcript: the execution it belongs to, its task and bot, the
// runtime's session id, the size and SHA-256 of what is kept, when it
// came, its place in the order transcripts came (Seq, from 1), the
// cursor of ListTranscripts, and, when a TranscriptFilter changed it,
// Filtered and the SHA-256 of what the node sent (SentSHA256).
type TranscriptRecord struct {
	Seq        int64              `json:"seq"`
	Execution  domain.ExecutionID `json:"executionId"`
	Task       domain.TaskID      `json:"taskId"`
	Bot        domain.BotID       `json:"botId"`
	Session    string             `json:"session"`
	Size       int64              `json:"size"`
	SHA256     string             `json:"sha256"`
	Filtered   bool               `json:"filtered,omitempty"`
	SentSHA256 string             `json:"sentSha256,omitempty"`
	ReceivedAt time.Time          `json:"receivedAt"`
}
