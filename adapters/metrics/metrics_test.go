package metrics

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/expfmt"

	"github.com/underpass-ai/axlr-bots/application"
)

type fakeSource struct {
	overview application.Overview
	err      error
}

func (f fakeSource) GetOverview(context.Context, time.Time) (application.Overview, error) {
	return f.overview, f.err
}

func scrape(t *testing.T, registry *prometheus.Registry) string {
	t.Helper()
	families, err := registry.Gather()
	if err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	for _, family := range families {
		if _, err := expfmt.MetricFamilyToText(&out, family); err != nil {
			t.Fatal(err)
		}
	}
	return out.String()
}

func TestOverviewCollectorTurnsTheOverviewIntoGauges(t *testing.T) {
	registry := prometheus.NewRegistry()
	registry.MustRegister(NewOverviewCollector(fakeSource{overview: application.Overview{
		Nodes:            map[string]int{"Online": 2},
		Bots:             map[string]int{"Enabled": 3},
		Tasks:            map[string]int{"Running": 1, "Failed": 4},
		TasksSince:       map[string]int{"Failed": 1},
		ExecutionsSince:  5,
		CostUSDSince:     1.25,
		CostUnknownSince: 2,
	}}))
	body := scrape(t, registry)
	for _, want := range []string{
		"axlr_bots_overview_up 1",
		`axlr_bots_nodes{state="Online"} 2`,
		`axlr_bots_bots{state="Enabled"} 3`,
		`axlr_bots_tasks{state="Failed"} 4`,
		`axlr_bots_tasks_today{state="Failed"} 1`,
		"axlr_bots_executions_today 5",
		"axlr_bots_cost_usd_today 1.25",
		"axlr_bots_executions_cost_unknown_today 2",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("lacks %s:\n%s", want, body)
		}
	}
}

func TestOverviewCollectorSaysDownWhenTheOverviewFails(t *testing.T) {
	registry := prometheus.NewRegistry()
	registry.MustRegister(NewOverviewCollector(fakeSource{err: errors.New("disk gone")}))
	body := scrape(t, registry)
	if !strings.Contains(body, "axlr_bots_overview_up 0") || strings.Contains(body, "axlr_bots_tasks") {
		t.Errorf("want up 0 and nothing else:\n%s", body)
	}
}

func TestHTTPInstrumentLabelsByPatternAndLeavesLongRequestsUntimed(t *testing.T) {
	registry := prometheus.NewRegistry()
	instrument := NewHTTPInstrument(registry)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/tasks/{id}", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) })
	mux.HandleFunc("GET /v1/nodes/{id}/stream", func(w http.ResponseWriter, _ *http.Request) {
		if _, ok := w.(http.Flusher); !ok {
			t.Error("the stream lost its flusher")
		}
		_, _ = io.WriteString(w, "event\n")
	})
	server := httptest.NewServer(instrument.Wrap("nodes", mux))
	defer server.Close()
	for _, path := range []string{"/v1/tasks/task-abc123", "/v1/nodes/node-1/stream", "/nowhere"} {
		response, err := http.Get(server.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		_ = response.Body.Close()
	}
	body := scrape(t, registry)
	for _, want := range []string{
		`axlr_bots_http_requests_total{code="404",listener="nodes",route="GET /v1/tasks/{id}"} 1`,
		`axlr_bots_http_requests_total{code="200",listener="nodes",route="GET /v1/nodes/{id}/stream"} 1`,
		`axlr_bots_http_requests_total{code="404",listener="nodes",route="unmatched"} 1`,
		`axlr_bots_http_request_duration_seconds_count{code="404",listener="nodes",route="GET /v1/tasks/{id}"} 1`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("lacks %s:\n%s", want, body)
		}
	}
	if strings.Contains(body, `duration_seconds_count{code="200",listener="nodes",route="GET /v1/nodes/{id}/stream"}`) {
		t.Error("the stream was timed")
	}
	if strings.Contains(body, "task-abc123") {
		t.Error("a task id became a label")
	}
}
