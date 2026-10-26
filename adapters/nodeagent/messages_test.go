package nodeagent

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/underpass-ai/axlr-bots/adapters/axlr"
	"github.com/underpass-ai/axlr-bots/adapters/unixapi"
	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/internal/fakeaxlr"
)

// The operator writes to a running task; the node reads the message with
// its lease and hands it to the run's --steer-file, and the run answers it.
func TestAnOperatorMessageReachesTheRunningExecution(t *testing.T) {
	f := control(t, "completed")
	ctx := context.Background()
	bin, argsFile, err := fakeaxlr.Write(f.dir, fakeaxlr.Options{Scenario: "steered", Steer: true})
	if err != nil {
		t.Fatal(err)
	}
	executor := axlr.NewExecutor(bin, f.nodeDir)
	agent := Agent{Transport: f.client, Executor: executor, Options: Options{NodeID: "web-1", HostName: "web-1", Heartbeat: 50 * time.Millisecond, PollWait: 100 * time.Millisecond, Renew: 30 * time.Millisecond, Log: log.New(f.logs, "", 0)}}
	agentCtx, stop := context.WithCancel(context.Background())
	defer stop()
	go func() { _ = agent.Run(agentCtx) }()
	if _, err := f.client.RegisterBot(ctx, application.BotDefinition{Tenant: "t", Name: "bot", Owner: "o", Mission: "do what each task asks", AllowedTools: []string{"local_read"}, Budgets: application.BudgetsDefinition{TaskTimeoutSeconds: 30}}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.client.EnableBot(ctx, "bot"); err != nil {
		t.Fatal(err)
	}
	submitted, err := f.client.SubmitTask(ctx, unixapi.SubmitRequest{BotID: "bot", Objective: "patrol the quay", IdempotencyKey: "k1"})
	if err != nil {
		t.Fatal(err)
	}
	task := submitted.Task.ID
	waitFor(t, f.client, task, "Running")
	sent, err := f.client.SendTaskMessage(ctx, task, unixapi.MessageRequest{Text: "check row F too"})
	if err != nil || sent.ExecutionID == "" {
		t.Fatalf("%+v %v", sent, err)
	}
	waitFor(t, f.client, task, "Completed")
	executions, err := f.client.ListExecutions(ctx, task)
	if err != nil || len(executions) != 1 || executions[0].Result.Answer != "Heard: check row F too" {
		t.Fatalf("%+v %v\n%s", executions, err, f.logs.String())
	}
	events, err := f.client.ListTaskEvents(ctx, task, 0)
	if err != nil {
		t.Fatal(err)
	}
	said := 0
	for _, event := range events {
		if event.Type == "bot.task.said" {
			said++
			if event.Payload["text"] != "Heard: check row F too" || event.Payload["inReplyTo"] != "check row F too" || event.ExecutionID.String() != sent.ExecutionID.String() {
				t.Fatalf("%+v", event)
			}
		}
	}
	if said != 1 {
		t.Fatalf("said %d times: %+v", said, events)
	}
	args, _ := os.ReadFile(argsFile)
	if !strings.Contains(string(args), "--steer-file\n"+filepath.Join(f.nodeDir, "steer")) {
		t.Fatalf("args = %s", args)
	}
	if _, err := os.Stat(filepath.Join(f.nodeDir, "steer", sent.ExecutionID.String()+".jsonl")); !os.IsNotExist(err) {
		t.Fatalf("the steer file stays after the run: %v", err)
	}
	if !strings.Contains(f.logs.String(), "1 message(s) from the operator handed to the run") {
		t.Fatalf("logs: %s", f.logs.String())
	}
	if _, err := f.client.SendTaskMessage(ctx, task, unixapi.MessageRequest{Text: "still there?"}); err == nil {
		t.Fatal("a finished task takes no message")
	}
}

// An axlr-tui without --steer-file gets no flag; a message to its run is
// logged and dropped, and the run ends as before.
func TestAnOlderRuntimeDropsTheMessage(t *testing.T) {
	dir := t.TempDir()
	bin, argsFile, err := fakeaxlr.Write(dir, fakeaxlr.Options{Scenario: "completed"})
	_ = os.WriteFile(argsFile, nil, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	executor := axlr.NewExecutor(bin, filepath.Join(dir, "node"))
	if executor.SupportsSteer(context.Background()) {
		t.Fatal("the stand-in advertises no -steer-file")
	}
	if err := executor.Steer(context.Background(), "exec-1", []string{"hi"}); err != axlr.ErrSteerUnsupported {
		t.Fatalf("%v", err)
	}
	if args, _ := os.ReadFile(argsFile); strings.Contains(string(args), "--steer-file") {
		t.Fatalf("args = %s", args)
	}
}
