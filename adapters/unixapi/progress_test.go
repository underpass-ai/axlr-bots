package unixapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

// A node tells what its run does; the task's timeline shows it at once,
// and a reader waiting on the timeline wakes for it.
func TestProgressOverTheSocketWakesTheTasksTimeline(t *testing.T) {
	client, local, _ := serve(t, "completed")
	ctx := context.Background()
	assignment := delegatingPair(t, client, client, "web-1")
	task := assignment.Run.TaskID
	timeline, err := client.ListTaskEvents(ctx, application.TaskEventsQuery{ID: task})
	if err != nil || len(timeline) == 0 {
		t.Fatalf("%+v %v", timeline, err)
	}
	last := timeline[len(timeline)-1].ID.String()

	server := httptest.NewServer(NewServer(local).OperatorHandler("secret-token-0123456789"))
	defer server.Close()
	get := func(query string) (int, []domain.Event, time.Duration) {
		request, _ := http.NewRequest(http.MethodGet, server.URL+"/v1/tasks/"+task.String()+"/events"+query, nil)
		request.Header.Set("Authorization", "Bearer secret-token-0123456789")
		started := time.Now()
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		var events []domain.Event
		_ = json.NewDecoder(response.Body).Decode(&events)
		return response.StatusCode, events, time.Since(started)
	}
	if code, events, took := get("?after=" + last + "&wait=1200ms"); code != http.StatusOK || len(events) != 0 || took < time.Second {
		t.Errorf("empty wait = %d %d %s", code, len(events), took)
	}
	go func() {
		time.Sleep(300 * time.Millisecond)
		answer, err := client.Progress(ctx, "web-1", assignment.ExecutionID, application.ProgressBatch{Items: []application.ProgressItem{
			{Kind: application.ProgressToolStarted, Tool: "local_read", CallID: "c1"},
			{Kind: application.ProgressText, Text: "Reading the log."},
		}})
		if err != nil || !answer.Recorded {
			t.Errorf("progress: %+v %v", answer, err)
		}
	}()
	code, events, took := get("?after=" + last + "&wait=10s")
	if code != http.StatusOK || len(events) != 1 || events[0].Type != domain.EventTaskProgress || events[0].ExecutionID != assignment.ExecutionID || took > 5*time.Second {
		t.Fatalf("woken wait = %d %+v %s", code, events, took)
	}
	items, _ := events[0].Payload["items"].([]any)
	if len(items) != 2 || events[0].CorrelationID == "" {
		t.Errorf("items = %+v", events[0])
	}
	for _, bad := range []string{"?wait=soon", "?limit=x"} {
		if code, _, _ := get(bad); code != http.StatusBadRequest {
			t.Errorf("%s: %d", bad, code)
		}
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
