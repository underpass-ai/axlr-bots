package daemon

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/underpass-ai/axlr-bots/adapters/logline"
	"github.com/underpass-ai/axlr-bots/application"
)

func TestJSONLineOfARunNamesBotTaskAndExecutionAsFields(t *testing.T) {
	f := newFixture(t, "completed")
	loggers, err := logline.New(f.logs, logline.FormatJSON)
	if err != nil {
		t.Fatal(err)
	}
	client, stop := f.startWith(t, func(o *Options) { o.Log = loggers.Info; o.ErrorLog = loggers.Error })
	defer stop()
	ctx := context.Background()
	if _, err := client.RegisterBot(ctx, application.BotDefinition{Tenant: "ops", Name: "bot", Owner: "platform", Mission: "diagnose", AllowedTools: []string{"local_read"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.EnableBot(ctx, "bot"); err != nil {
		t.Fatal(err)
	}
	submitted, err := client.SubmitTask(ctx, application.SubmitRequest{BotID: "bot", Objective: "list the files", IdempotencyKey: "k-1"})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, client, submitted.Task.ID, "Completed")
	deadline := time.Now().Add(3 * time.Second)
	for !strings.Contains(f.logs.String(), "Completed (execution") && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	for _, raw := range strings.Split(strings.TrimSpace(f.logs.String()), "\n") {
		var line map[string]any
		if err := json.Unmarshal([]byte(raw), &line); err != nil {
			t.Fatalf("not JSON: %s", raw)
		}
		if msg, _ := line["msg"].(string); strings.Contains(msg, "Completed (execution") {
			if line["bot"] != "bot" || line["task"] != submitted.Task.ID || line["execution"] == nil || line["component"] != "daemon" {
				t.Errorf("run line: %v", line)
			}
			return
		}
	}
	t.Fatalf("no run line:\n%s", f.logs.String())
}

func TestLevelLoggerCarriesTheSubjectOnBothLevels(t *testing.T) {
	var out strings.Builder
	loggers, _ := logline.New(&out, logline.FormatJSON)
	logger := levelLogger{info: loggers.Info, errors: loggers.Error}
	scoped := application.LogWith(logger, application.LogSubject{Bot: "bot"})
	scoped.Printf("daemon: fine")
	errorf(scoped, "daemon: broken")
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 2 || !strings.Contains(lines[0], `"bot":"bot"`) || !strings.Contains(lines[1], `"level":"ERROR"`) || !strings.Contains(lines[1], `"bot":"bot"`) {
		t.Errorf("lines: %v", lines)
	}
}
