package unixapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

// A node tells what its run does; the task's timeline shows it at once,
// and the task's timeline shows what it did.
func TestProgressOverTheSocketShowsOnTheTasksTimeline(t *testing.T) {
	client, local, _ := serve(t, "completed")
	ctx := context.Background()
	assignment := delegatingPair(t, local, client, "web-1")
	task := assignment.Run.TaskID
	timeline, err := local.ListTaskEvents(ctx, application.TaskEventsQuery{ID: task})
	if err != nil || len(timeline) == 0 {
		t.Fatalf("%+v %v", timeline, err)
	}
	last := timeline[len(timeline)-1].ID.String()

	answer, err := client.Progress(ctx, "web-1", assignment.ExecutionID, application.ProgressBatch{Items: []application.ProgressItem{
		{Kind: application.ProgressToolStarted, Tool: "local_read", CallID: "c1"},
		{Kind: application.ProgressText, Text: "Reading the log."},
	}})
	if err != nil || !answer.Recorded {
		t.Fatalf("progress: %+v %v", answer, err)
	}
	events, err := local.ListTaskEvents(ctx, application.TaskEventsQuery{ID: task, After: last})
	if err != nil || len(events) != 1 || events[0].Type != domain.EventTaskProgress || events[0].ExecutionID != assignment.ExecutionID {
		t.Fatalf("timeline after the progress = %+v %v", events, err)
	}
	items, _ := events[0].Payload["items"].([]any)
	if len(items) != 2 || events[0].CorrelationID == "" {
		t.Errorf("items = %+v", events[0])
	}

	if _, err := client.Progress(ctx, "web-2", assignment.ExecutionID, application.ProgressBatch{Items: []application.ProgressItem{{Kind: application.ProgressText, Text: "x"}}}); err == nil {
		t.Error("another node's execution is refused")
	}
	if _, err := client.Progress(ctx, "web-1", assignment.ExecutionID, application.ProgressBatch{}); err == nil || !strings.Contains(err.Error(), "items") {
		t.Errorf("an empty batch: %v", err)
	}
	if _, err := client.Report(ctx, "web-1", assignment.ExecutionID, application.ExecutionReport{Outcome: domain.ExecutionCompleted, Result: domain.Result{Answer: "done"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Progress(ctx, "web-1", assignment.ExecutionID, application.ProgressBatch{Items: []application.ProgressItem{{Kind: application.ProgressText, Text: "late"}}}); err == nil {
		t.Error("an ended execution is refused")
	}
}

// A control plane older than the route answers its mux's plain 404: the
// node reads it as not found and stops sending.
func TestProgressToAControlPlaneWithoutTheRoute(t *testing.T) {
	old := httptest.NewServer(http.NotFoundHandler())
	defer old.Close()
	client := NewClient(old.URL)
	_, err := client.Progress(context.Background(), "web-1", "exec-1", application.ProgressBatch{Items: []application.ProgressItem{{Kind: application.ProgressText, Text: "x"}}})
	if !domain.IsNotFound(err) {
		t.Fatalf("%v", err)
	}
}
