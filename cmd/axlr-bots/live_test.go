package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/underpass-ai/axlr-bots/adapters/unixapi"
	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

// TestLiveWebhookRunsOnTheNode fires a webhook against the control plane
// running on this machine (the installed service, through its socket)
// and waits for the node to complete the task with the real axlr-tui and
// a model. It runs only with AXLR_BOTS_LIVE=1; AXLR_BOTS_SOCKET names the
// socket (default ~/.local/state/axlr-bots/axlr-bots.sock).
func TestLiveWebhookRunsOnTheNode(t *testing.T) {
	if os.Getenv("AXLR_BOTS_LIVE") != "1" {
		t.Skip("set AXLR_BOTS_LIVE=1 to fire a webhook against the running daemon")
	}
	socket := os.Getenv("AXLR_BOTS_SOCKET")
	if socket == "" {
		home, _ := os.UserHomeDir()
		socket = filepath.Join(home, ".local", "state", "axlr-bots", "axlr-bots.sock")
	}
	ctx := context.Background()
	if !unixapi.Reachable(ctx, socket) {
		t.Fatalf("no daemon answers on %s", socket)
	}
	client := unixapi.NewClient(socket)
	const secret = "live-webhook-secret-0123456789"
	triggers := []domain.TriggerSnapshot{
		{Type: "manual"},
		{Type: "webhook", Name: "deploy", Objective: "Handle event {{.type}} for {{.service}}: say in one sentence what you would check first, without running anything.", Secret: secret},
	}
	definition := application.BotDefinition{
		ID: "live-webhook", Tenant: "default", Name: "live-webhook", Owner: "operator",
		Mission:      "Do what each task asks, within the policy.",
		Level:        "observe",
		AllowedTools: []string{"local_read", "local_search", "local_list"},
		Triggers:     triggers,
		Model:        application.ModelPolicyDefinition{Model: "anthropic/claude-haiku-5.5", RoutingPolicy: "cost-optimized", AllowRemoteProviders: true, MaxContextTokens: 32000},
		Budgets:      application.BudgetsDefinition{MaxModelCallsPerTask: 10, MaxToolCallsPerTask: 20, TaskTimeoutSeconds: 300},
	}
	if _, err := client.RegisterBot(ctx, definition); err != nil {
		if !strings.Contains(err.Error(), "already exists") {
			t.Fatal(err)
		}
		if _, err := client.UpdateBot(ctx, "live-webhook", application.BotPatch{Triggers: &triggers}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := client.EnableBot(ctx, "live-webhook"); err != nil && !strings.Contains(err.Error(), "Enabled") {
		t.Fatal(err)
	}
	delivery := "live-" + time.Now().UTC().Format("20060102T150405Z")
	body := json.RawMessage(`{"type": "deploy", "service": "web"}`)
	fired, err := client.FireTrigger(ctx, unixapi.FireRequest{BotID: "live-webhook", Trigger: "deploy", DeliveryID: delivery, Body: body})
	if err != nil || !fired.Created {
		t.Fatalf("fire: %v %+v", err, fired)
	}
	again, err := client.FireTrigger(ctx, unixapi.FireRequest{BotID: "live-webhook", Trigger: "deploy", DeliveryID: delivery, Body: body})
	if err != nil || again.Created || again.Task.ID != fired.Task.ID {
		t.Errorf("redelivery: %v %+v", err, again)
	}
	deadline := time.Now().Add(3 * time.Minute)
	var task domain.TaskSnapshot
	for {
		task, err = client.GetTask(ctx, fired.Task.ID)
		if err == nil && (task.Status == "Completed" || task.Status == "Failed") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("task %s is %s after 3 minutes", fired.Task.ID, task.Status)
		}
		time.Sleep(2 * time.Second)
	}
	executions, err := client.ListExecutions(ctx, fired.Task.ID)
	if err != nil || len(executions) != 1 {
		t.Fatalf("executions: %v %d", err, len(executions))
	}
	execution := executions[0]
	if task.Status != "Completed" || execution.NodeID == "" || execution.Correlation == nil || execution.Correlation.TraceID != task.CorrelationID {
		t.Errorf("task %s on node %q, correlation %+v, reason %q", task.Status, execution.NodeID, execution.Correlation, execution.FailureReason)
	}
	report, err := client.GetReport(ctx, fired.Task.ID)
	if err != nil || report.Content == "" {
		t.Errorf("report: %v", err)
	}
	t.Logf("task %s %s on node %s in %s, $%.5f, carried=%v, report: %s", task.ID, task.Status, execution.NodeID, execution.EndedAt.Sub(execution.StartedAt).Round(time.Second), execution.Usage.CostUSD, execution.Correlation.Carried, strings.TrimSpace(report.Content))
}
