package grpcapi

import (
	"time"

	operatorv1 "github.com/underpass-ai/axlr-bots/api/gen/axlrbots/operator/v1"
	"github.com/underpass-ai/axlr-bots/application"
	"google.golang.org/protobuf/types/known/durationpb"
)

// activityItem maps a progress item to the wire.
func activityItem(i application.ProgressItem) *operatorv1.ProgressItem {
	return &operatorv1.ProgressItem{
		Kind: string(i.Kind), At: timestamp(i.At), Tool: i.Tool, McpTool: i.MCPTool, CallId: i.CallID,
		Summary: i.Summary, Status: i.Status, Error: i.Error,
		Duration: durationpb.New(time.Duration(i.DurationMS) * time.Millisecond),
		Bot:      i.Bot, TaskId: i.TaskID, Reason: i.Reason, Text: i.Text,
	}
}

func activityItems(items []application.ProgressItem) []*operatorv1.ProgressItem {
	out := make([]*operatorv1.ProgressItem, len(items))
	for i, item := range items {
		out[i] = activityItem(item)
	}
	return out
}

// activityCut maps a progress cut to the wire.
func activityCut(c application.ProgressCut) *operatorv1.ProgressCut {
	return &operatorv1.ProgressCut{
		Reason: activityCutReason(c.Reason), EventsKept: int32(c.EventsKept), BytesKept: c.BytesKept,
		CutAt: timestamp(c.CutAt), Dropped: int32(c.Dropped), Completion: activityCompletion(c.Completion),
		TranscriptSessions: c.TranscriptSessions, CompletionBounded: c.CompletionBounded,
	}
}

func activityCutReason(r application.ProgressCutReason) operatorv1.ProgressCutReason {
	switch r {
	case application.CutEventLimit:
		return operatorv1.ProgressCutReason_PROGRESS_CUT_REASON_EVENT_LIMIT
	case application.CutByteLimit:
		return operatorv1.ProgressCutReason_PROGRESS_CUT_REASON_BYTE_LIMIT
	case application.CutBatchTruncated:
		return operatorv1.ProgressCutReason_PROGRESS_CUT_REASON_BATCH_TRUNCATED
	}
	return operatorv1.ProgressCutReason_PROGRESS_CUT_REASON_UNSPECIFIED
}

func activityCompletion(s application.CompletionSource) operatorv1.CompletionSource {
	switch s {
	case application.CompletionNone:
		return operatorv1.CompletionSource_COMPLETION_SOURCE_NONE
	case application.CompletionTranscript:
		return operatorv1.CompletionSource_COMPLETION_SOURCE_TRANSCRIPT
	}
	return operatorv1.CompletionSource_COMPLETION_SOURCE_UNSPECIFIED
}
