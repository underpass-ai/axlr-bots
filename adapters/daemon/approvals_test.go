package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/underpass-ai/axlr-bots/adapters/unixapi"
	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

// movableClock is a clock the test moves forward while the daemon reads it.
type movableClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *movableClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *movableClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

func TestDaemonNotifiesThenExpiresAnApprovalAndRefusesTheLateDecision(t *testing.T) {
	f := newFixture(t, "approval-flow")
	clock := &movableClock{now: time.Now()}
	f.deps.Clock = clock
	notified := filepath.Join(f.dir, "notified.json")
	client, stop := f.start(t)
	defer stop()
	ctx := context.Background()
	definition := application.BotDefinition{
		Tenant: "t", Name: "writer", Owner: "o", Mission: "do what each task asks", Level: "act-with-approval", AllowedTools: []string{"local_read", "local_write"},
		Approvals: domain.ApprovalsSnapshot{TTL: "2m", Notify: []domain.NotifyTargetSnapshot{{Type: "command", Name: "file", Command: []string{"/bin/sh", "-c", `cat > "$1"`, "notify", notified}}}},
	}
	if _, err := client.RegisterBot(ctx, definition); err != nil {
		t.Fatal(err)
	}
	if _, err := client.EnableBot(ctx, "writer"); err != nil {
		t.Fatal(err)
	}
	submitted, err := client.SubmitTask(ctx, unixapi.SubmitRequest{BotID: "writer", Objective: "restart the unit", IdempotencyKey: "k"})
	if err != nil {
		t.Fatal(err)
	}
	waiting := waitFor(t, client, submitted.Task.ID, "WaitingApproval")
	if waiting.Approval == nil || !waiting.Approval.Open() || waiting.Approval.CallID != "c1" {
		t.Fatalf("approval = %+v", waiting.Approval)
	}
	// The command target receives the request on its stdin.
	deadline := time.Now().Add(5 * time.Second)
	var notification application.ApprovalNotification
	for {
		raw, err := os.ReadFile(notified)
		if err == nil && json.Unmarshal(raw, &notification) == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("not notified: %s", f.logs.String())
		}
		time.Sleep(10 * time.Millisecond)
	}
	if notification.TaskID != submitted.Task.ID || notification.Call.CallID != "c1" || !notification.Deadline.Equal(waiting.Approval.Deadline) || notification.Actions.Approve.Path == "" {
		t.Errorf("notification = %+v", notification)
	}
	clock.advance(2*time.Minute + time.Second)
	completed := waitFor(t, client, submitted.Task.ID, "Completed")
	if completed.Approval == nil || completed.Approval.Outcome != domain.ApprovalExpired || len(completed.ExecutionIDs) != 2 {
		t.Errorf("task = %+v", completed)
	}
	executions, _ := client.ListExecutions(ctx, submitted.Task.ID)
	if len(executions) != 2 || executions[1].Decision != "deny" || !strings.Contains(executions[1].Result.Answer, "denied") {
		t.Errorf("executions = %+v", executions)
	}
	_, err = client.DecideTask(ctx, submitted.Task.ID, unixapi.DecideRequest{Decision: "approve"})
	var remote unixapi.RemoteError
	if !errors.As(err, &remote) || remote.Kind != unixapi.KindApprovalExpired {
		t.Errorf("late approve: %v", err)
	}
	events, _ := client.ListTaskEvents(ctx, submitted.Task.ID, 0)
	var types []string
	for _, e := range events {
		types = append(types, e.Type.String())
	}
	if joined := strings.Join(types, ","); !strings.Contains(joined, "bot.task.waiting_approval,bot.approval.expired,bot.task.decided") {
		t.Errorf("events = %s", joined)
	}
	for _, want := range []string{"expired: deny", "to file: delivered (attempt 1)"} {
		if !strings.Contains(f.logs.String(), want) {
			t.Errorf("logs lack %q:\n%s", want, f.logs.String())
		}
	}
}
