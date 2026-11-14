package nodeagent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/underpass-ai/axlr-bots/adapters/logline"
	"github.com/underpass-ai/axlr-bots/application"
)

// jsonLines are the lines the buffer holds, each decoded.
func jsonLines(t *testing.T, text string) []map[string]any {
	t.Helper()
	var lines []map[string]any
	for _, raw := range strings.Split(strings.TrimSpace(text), "\n") {
		if raw == "" {
			continue
		}
		var line map[string]any
		if err := json.Unmarshal([]byte(raw), &line); err != nil {
			t.Fatalf("not JSON: %s", raw)
		}
		lines = append(lines, line)
	}
	return lines
}

func TestJSONLinesOfAnExecutionCarryItsSubjectAsFields(t *testing.T) {
	f := control(t, "completed")
	ctx := context.Background()
	agent := f.agent(t, "completed")
	loggers, err := logline.New(f.logs, logline.FormatJSON)
	if err != nil {
		t.Fatal(err)
	}
	agent.Options.Log = loggers.Info
	agentCtx, stop := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- agent.Run(agentCtx) }()
	if _, err := f.client.RegisterBot(ctx, application.BotDefinition{Tenant: "t", Name: "bot", Owner: "o", Mission: "do what each task asks", Level: "act-with-approval", AllowedTools: []string{"local_read"}, Budgets: application.BudgetsDefinition{TaskTimeoutSeconds: 30}}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.client.EnableBot(ctx, "bot"); err != nil {
		t.Fatal(err)
	}
	submitted, err := f.client.SubmitTask(ctx, application.SubmitRequest{BotID: "bot", Objective: "list the files", IdempotencyKey: "k-1"})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, f.client, submitted.Task.ID, "Completed")
	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(f.logs.String(), "reported Completed") {
		if time.Now().After(deadline) {
			t.Fatalf("no report line:\n%s", f.logs.String())
		}
		time.Sleep(10 * time.Millisecond)
	}
	stop()
	<-done
	var registered, assigned, reported map[string]any
	for _, line := range jsonLines(t, f.logs.String()) {
		msg, _ := line["msg"].(string)
		switch {
		case strings.Contains(msg, "registered as"):
			registered = line
		case strings.Contains(msg, "assigned, lease until"):
			assigned = line
		case strings.Contains(msg, "reported Completed"):
			reported = line
		}
	}
	if registered == nil || registered["node"] != "web-1" || registered["bot"] != nil || registered["execution"] != nil {
		t.Errorf("the registration is about the node only: %v", registered)
	}
	for name, line := range map[string]map[string]any{"assigned": assigned, "reported": reported} {
		if line == nil {
			t.Fatalf("no %s line:\n%s", name, f.logs.String())
		}
		if line["bot"] != "bot" || line["node"] != "web-1" || line["task"] != submitted.Task.ID || line["execution"] == nil || line["component"] != "node" {
			t.Errorf("%s line: %v", name, line)
		}
	}
	if assigned["execution"] != reported["execution"] {
		t.Errorf("one execution, two ids: %v %v", assigned["execution"], reported["execution"])
	}
}

func TestExecutionSubjectsRememberAndForget(t *testing.T) {
	var nobody *executionSubjects
	nobody.add("exec-1", application.LogSubject{Bot: "b"})
	nobody.remove("exec-1")
	if nobody.of("exec-1") != (application.LogSubject{}) {
		t.Error("a nil registry remembered")
	}
	subjects := newExecutionSubjects()
	subjects.add("exec-1", application.LogSubject{Bot: "b"})
	if subjects.of("exec-1").Bot != "b" {
		t.Error("not remembered")
	}
	subjects.remove("exec-1")
	if subjects.of("exec-1").Bot != "" {
		t.Error("not forgotten")
	}
}
