package application

import (
	"context"
	"encoding/json"

	"github.com/underpass-ai/axlr-bots/domain"
)

// Bounds of what the control plane keeps of an execution's progress: a
// batch's items, and the events and bytes per execution. Past them the
// run goes on and its report still says everything; the timeline only
// stops growing.
const (
	MaxProgressItems  = 64
	MaxProgressEvents = 400
	MaxProgressBytes  = 256 << 10
)

// RecordProgress takes what a node read from a running execution's
// lines: one bot.task.progress event in the task's timeline per batch,
// so the operator watches the run as it goes. A node that does not hold
// the execution, or an execution that ended, is refused, as for
// RecordSaid. An execution past its bound is answered Full and nothing
// more is recorded.
type RecordProgress struct {
	deps Dependencies
	rec  recorder
}

// NewRecordProgress wires the use case.
func NewRecordProgress(deps Dependencies) RecordProgress {
	return RecordProgress{deps: deps, rec: recorder{deps: deps}}
}

// Execute records the batch.
func (uc RecordProgress) Execute(ctx context.Context, node domain.NodeID, id domain.ExecutionID, batch ProgressBatch) (ProgressAnswer, error) {
	if len(batch.Items) > MaxProgressItems {
		return ProgressAnswer{}, domain.ValidationError{Field: "items", Reason: "at most 64 in a batch"}
	}
	now := uc.deps.Clock.Now()
	items := make([]ProgressItem, 0, len(batch.Items))
	for _, item := range batch.Items {
		if kept, ok := item.normalized(now); ok {
			items = append(items, kept)
		}
	}
	dropped := max(batch.Dropped, 0)
	if len(items) == 0 && dropped == 0 {
		return ProgressAnswer{}, domain.ValidationError{Field: "items", Reason: "must hold an item of a known kind"}
	}
	execution, err := uc.deps.Executions.Get(ctx, id)
	if err != nil {
		return ProgressAnswer{}, err
	}
	if execution.NodeID() != node || execution.Outcome().IsFinal() {
		return ProgressAnswer{}, domain.StateError{Subject: "execution " + id.String(), Current: "not running on node " + node.String(), Attempted: "tell its progress"}
	}
	events, size, ended, err := uc.recorded(ctx, execution)
	if err != nil {
		return ProgressAnswer{}, err
	}
	if ended || events >= MaxProgressEvents || size >= MaxProgressBytes {
		return ProgressAnswer{Full: true}, nil
	}
	// The last event the bound allows keeps what fits and says the rest
	// was cut.
	truncated := events == MaxProgressEvents-1
	for len(items) > 0 && size+itemsSize(items) > MaxProgressBytes {
		items, truncated = items[:len(items)-1], true
	}
	task, err := uc.deps.Tasks.Get(ctx, execution.TaskID())
	if err != nil {
		return ProgressAnswer{}, err
	}
	bot, err := uc.deps.Bots.Get(ctx, task.BotID())
	if err != nil {
		return ProgressAnswer{}, err
	}
	payload := map[string]any{"items": items}
	if dropped > 0 {
		payload["dropped"] = dropped
	}
	if truncated {
		payload["truncated"] = true
	}
	if err := uc.rec.executionEvent(ctx, domain.EventTaskProgress, bot, task.ID(), id, task.CorrelationID(), payload); err != nil {
		return ProgressAnswer{}, err
	}
	return ProgressAnswer{Recorded: true, Full: truncated}, nil
}

// recorded counts the progress events the execution has in its task's
// timeline and the bytes of their items, and says whether the last the
// bound allowed is among them.
func (uc RecordProgress) recorded(ctx context.Context, execution domain.Execution) (int, int, bool, error) {
	timeline, err := uc.deps.EventReader.ListTaskEvents(ctx, execution.TaskID(), MaxEventLimit)
	if err != nil {
		return 0, 0, false, err
	}
	events, size, ended := 0, 0, false
	for _, event := range timeline {
		if event.Type != domain.EventTaskProgress || event.ExecutionID != execution.ID() {
			continue
		}
		events++
		raw, _ := json.Marshal(event.Payload["items"])
		size += len(raw)
		ended = ended || event.Payload["truncated"] == true
	}
	return events, size, ended, nil
}

// itemsSize is the bytes the items take in the timeline.
func itemsSize(items []ProgressItem) int {
	raw, _ := json.Marshal(items)
	return len(raw)
}
