package application

import "time"

// ProgressCut marks where an execution's recorded progress timeline stops.
type ProgressCut struct {
	Reason ProgressCutReason
	// EventsKept and BytesKept are what the timeline holds of the
	// execution's progress.
	EventsKept int
	BytesKept  int64
	// CutAt is the time of the last item the timeline kept.
	CutAt time.Time
	// Dropped counts the items the nodes said they dropped.
	Dropped    int
	Completion CompletionSource
	// TranscriptSessions are the transcripts the completion was read from.
	TranscriptSessions []string
	// CompletionBounded is true when MaxCompletedItems cut the completion.
	CompletionBounded bool
}
