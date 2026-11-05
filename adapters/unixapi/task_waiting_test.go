package unixapi

import (
	"context"
	"testing"

	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

// A placed bot's Pending task says what it waits for in every answer
// that carries it (submit, get, list), and says nothing once its node is
// Online and takes it.
func TestAPlacedTaskAnswersWhatItWaitsFor(t *testing.T) {
	client, _, _ := serve(t, "completed")
	ctx := context.Background()
	placed := definition("bot-a", "observe")
	placed.Placement = domain.PlacementSnapshot{Nodes: []string{"web-1"}}
	if _, err := client.RegisterBot(ctx, placed); err != nil {
		t.Fatal(err)
	}
	if _, err := client.EnableBot(ctx, "bot-a"); err != nil {
		t.Fatal(err)
	}
	const want = "waiting for node web-1, which is not registered"
	submitted, err := client.SubmitTask(ctx, SubmitRequest{BotID: "bot-a", Objective: "Look at the host", IdempotencyKey: "k-1"})
	if err != nil || submitted.Task.Waiting != want {
		t.Fatalf("submit: %v %q", err, submitted.Task.Waiting)
	}
	if task, err := client.GetTask(ctx, submitted.Task.ID); err != nil || task.Status != "Pending" || task.Waiting != want {
		t.Errorf("get: %v %+v", err, task)
	}
	if tasks, err := client.ListTasks(ctx, application.TaskQuery{}); err != nil || len(tasks) != 1 || tasks[0].Waiting != want {
		t.Errorf("list: %v %+v", err, tasks)
	}
	if _, err := client.Register(ctx, application.NodeRegistration{ID: "web-1", HostName: "web-1.example", Capabilities: domain.NodeCapabilities{AXLRVersion: "dev", PolicySupported: true}}); err != nil {
		t.Fatal(err)
	}
	if task, err := client.GetTask(ctx, submitted.Task.ID); err != nil || task.Waiting != "" {
		t.Errorf("with its node Online: %v %q", err, task.Waiting)
	}
}
