package application

import (
	"encoding/json"
	"time"

	"github.com/underpass-ai/axlr-bots/domain"
)

// recordedProgress is what the timeline holds of one execution's
// progress.
type recordedProgress struct {
	events    int
	bytes     int64
	truncated bool
	dropped   int
	items     []ProgressItem
	lastEvent time.Time
}

// progressOf reads an execution's bot.task.progress events out of its
// task's timeline, as RecordProgress counted them.
func progressOf(timeline []domain.Event, execution domain.ExecutionID) recordedProgress {
	var out recordedProgress
	for _, event := range timeline {
		if event.Type != domain.EventTaskProgress || event.ExecutionID != execution {
			continue
		}
		out.events++
		raw, _ := json.Marshal(event.Payload["items"])
		out.bytes += int64(len(raw))
		out.truncated = out.truncated || event.Payload["truncated"] == true
		out.dropped += payloadCount(event.Payload["dropped"])
		var items []ProgressItem
		_ = json.Unmarshal(raw, &items)
		out.items = append(out.items, items...)
		out.lastEvent = event.CreatedAt
	}
	return out
}

// payloadCount reads a number out of a payload, whatever it decoded to.
func payloadCount(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	}
	return 0
}

// cut says whether RecordProgress stopped recording and why; nil when
// the timeline is whole.
func (r recordedProgress) cut() *ProgressCut {
	var reason ProgressCutReason
	switch {
	case r.bytes >= MaxProgressBytes:
		reason = CutByteLimit
	case r.events >= MaxProgressEvents:
		reason = CutEventLimit
	case r.truncated:
		reason = CutBatchTruncated
	default:
		return nil
	}
	var cutAt time.Time
	for _, item := range r.items {
		if item.At.After(cutAt) {
			cutAt = item.At
		}
	}
	if cutAt.IsZero() {
		cutAt = r.lastEvent
	}
	return &ProgressCut{
		Reason: reason, EventsKept: r.events, BytesKept: r.bytes, CutAt: cutAt,
		Dropped: r.dropped, Completion: CompletionNone,
	}
}
