package taskactivity

import "time"

// Cut marks where an execution's recorded progress timeline stops.
type Cut struct {
	// Reason is event_limit, byte_limit or batch_truncated.
	Reason     string    `json:"reason"`
	EventsKept int       `json:"eventsKept"`
	BytesKept  int64     `json:"bytesKept"`
	CutAt      time.Time `json:"cutAt"`
	Dropped    int       `json:"dropped,omitempty"`
	// Completion is none or transcript: where the items after the cut
	// came from.
	Completion         string   `json:"completion"`
	TranscriptSessions []string `json:"transcriptSessions,omitempty"`
	CompletionBounded  bool     `json:"completionBounded,omitempty"`
}
