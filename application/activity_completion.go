package application

import (
	"context"
	"errors"
	"time"

	"github.com/underpass-ai/axlr-bots/domain"
)

// execution builds one execution's activity: its cut, when the progress
// timeline has one, and the content after it read from the transcript.
func (b *activityBuilder) execution(ctx context.Context, execution domain.Execution, timeline []domain.Event) (ExecutionActivity, error) {
	out := ExecutionActivity{Execution: execution}
	recorded := progressOf(timeline, execution.ID())
	cut := recorded.cut()
	if cut == nil {
		return out, nil
	}
	out.Cut = cut
	if b.options.SkipCompletedProgress || b.deps.Transcripts == nil || b.deps.SessionItems == nil {
		return out, nil
	}
	items, sessions, err := b.transcriptItems(ctx, execution)
	if err != nil {
		return ExecutionActivity{}, err
	}
	if len(sessions) == 0 {
		return out, nil
	}
	cut.Completion = CompletionTranscript
	cut.TranscriptSessions = sessions
	after := itemsAfter(items, recorded.items, cut.CutAt)
	limit := b.options.completedLimit()
	if len(after) > limit {
		after, cut.CompletionBounded = after[:limit], true
	}
	out.Completed = after
	return out, nil
}

// transcriptItems reads, in order, the items of every session the
// execution's result names that the control plane keeps; a session with
// no transcript is skipped, and the sessions answered are the ones read.
func (b *activityBuilder) transcriptItems(ctx context.Context, execution domain.Execution) ([]ProgressItem, []string, error) {
	var items []ProgressItem
	var read []string
	for _, session := range sessionsOf(execution.Result()) {
		_, content, err := b.deps.Transcripts.LoadTranscript(ctx, execution.ID(), session)
		if err != nil {
			var missing domain.NotFoundError
			if errors.As(err, &missing) {
				continue
			}
			return nil, nil, err
		}
		parsed, err := b.deps.SessionItems.ReadItems(content)
		if err != nil {
			// A transcript that cannot be read leaves the cut marked and
			// the rest of the answer whole.
			continue
		}
		items = append(items, parsed...)
		read = append(read, session)
	}
	return items, read, nil
}

// sessionsOf lists the result's sessions once each, the one the run
// ended on included.
func sessionsOf(result domain.Result) []string {
	var out []string
	seen := map[string]bool{}
	for _, s := range append(append([]string(nil), result.Sessions...), result.SessionID) {
		if s != "" && !seen[s] && validSession(s) == nil {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// itemsAfter is what the transcript holds past what the timeline kept.
// The timeline's last call item is found in the transcript by its kind
// and call id and everything after it is the completion; with no such
// item (the timeline holds words only, or nothing) the items after the
// time of the last item kept are. Words and reasoning the timeline
// already kept up to cutAt are not repeated.
func itemsAfter(transcript, kept []ProgressItem, cutAt time.Time) []ProgressItem {
	anchor := -1
	seen := map[string]bool{}
	for _, item := range kept {
		if item.CallID != "" {
			seen[string(item.Kind)+"/"+item.CallID] = true
		}
	}
	for i, item := range transcript {
		if item.CallID != "" && seen[string(item.Kind)+"/"+item.CallID] {
			anchor = i
		}
	}
	var after []ProgressItem
	switch {
	case anchor >= 0:
		after = transcript[anchor+1:]
	case len(kept) == 0:
		return transcript
	default:
		after = transcript
	}
	out := make([]ProgressItem, 0, len(after))
	for _, item := range after {
		if !item.At.IsZero() && !item.At.After(cutAt) && (anchor < 0 || item.Kind.Streamed()) {
			continue
		}
		out = append(out, item)
	}
	return out
}
