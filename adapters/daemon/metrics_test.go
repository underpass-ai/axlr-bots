package daemon

import (
	"context"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
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
		`axlr_bots_http_requests_total{code="200",listener="socket",route="GET /v1/bots"} 1`,
		`axlr_bots_http_request_duration_seconds_count{code="200",listener="socket",route="GET /v1/bots"} 1`,
		`route="GET /v1/tasks/{id}"`,
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
