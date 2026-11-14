package daemon

import (
	"context"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/underpass-ai/axlr-bots/application"
)

func TestDaemonServesMetricsOnItsOwnAddress(t *testing.T) {
	f := newFixture(t, "success")
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	client, stop := f.startWith(t, func(o *Options) { o.MetricsListener = listener })
	defer stop()
	if _, err := client.ListBots(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := client.GetTask(context.Background(), "task-000000000000000000000000"); err == nil {
		t.Fatal("an unknown task was found")
	}
	body := get(t, "http://"+listener.Addr().String()+"/metrics")
	for _, want := range []string{
		"axlr_bots_overview_up 1",
		`axlr_bots_tasks{state="Pending"} 0`,
		`axlr_bots_grpc_requests_total{code="OK",method="ListBots",service="axlrbots.operator.v1.BotsService"} 1`,
		`axlr_bots_grpc_request_duration_seconds_count{code="OK",method="ListBots",service="axlrbots.operator.v1.BotsService"} 1`,
		`axlr_bots_grpc_requests_total{code="NotFound",method="GetTask",service="axlrbots.operator.v1.TasksService"} 1`,
		`axlr_bots_http_requests_total{code="200",listener="socket",route="GET /healthz"}`,
		"go_goroutines",
		"process_cpu_seconds_total",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("metrics lack %s:\n%s", want, body)
		}
	}
	if strings.Contains(body, "task-000000000000000000000000") {
		t.Error("a task id became a label")
	}
	if got := get(t, "http://"+listener.Addr().String()+"/healthz"); got != "ok\n" {
		t.Errorf("healthz = %q", got)
	}
	if !strings.Contains(f.logs.String(), "/metrics") {
		t.Errorf("the start line does not name the metrics address:\n%s", f.logs.String())
	}
}

// A task the daemon runs shows in the per-bot task metrics: the event
// publisher is decorated where the daemon composes the service.
func TestDaemonCountsTheTasksItRunsPerBot(t *testing.T) {
	f := newFixture(t, "completed")
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	client, stop := f.startWith(t, func(o *Options) { o.MetricsListener = listener })
	defer stop()
	ctx := context.Background()
	if _, err := client.RegisterBot(ctx, application.BotDefinition{Tenant: "ops", Name: "bot", Owner: "platform", Mission: "diagnose", AllowedTools: []string{"local_read"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.EnableBot(ctx, "bot"); err != nil {
		t.Fatal(err)
	}
	submitted, err := client.SubmitTask(ctx, application.SubmitRequest{BotID: "bot", Objective: "List the files", IdempotencyKey: "k-1"})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, client, submitted.Task.ID, "Completed")
	deadline := time.Now().Add(5 * time.Second)
	var body string
	for time.Now().Before(deadline) {
		if body = get(t, "http://"+listener.Addr().String()+"/metrics"); strings.Contains(body, `axlr_bots_tasks_finished_total{bot="bot",state="Completed"} 1`) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	for _, want := range []string{
		`axlr_bots_tasks_finished_total{bot="bot",state="Completed"} 1`,
		`axlr_bots_cost_usd_total{bot="bot"} 0.001`,
		`axlr_bots_task_queue_seconds_count{bot="bot"} 1`,
		`axlr_bots_task_duration_seconds_count{bot="bot",state="Completed"} 1`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("metrics lack %s:\n%s", want, body)
		}
	}
}

func get(t *testing.T, url string) string {
	t.Helper()
	client := http.Client{Timeout: 5 * time.Second}
	response, err := client.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}
