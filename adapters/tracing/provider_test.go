package tracing

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestExporterRefusesAnEndpointThatIsNotHTTP(t *testing.T) {
	for _, endpoint := range []string{"tempo:4318", "grpc://tempo:4317", "http://", "://x", "http://%zz"} {
		if _, err := NewOTLPExporter(context.Background(), endpoint); err == nil || !strings.Contains(err.Error(), "OTLP endpoint") {
			t.Fatalf("%q: %v", endpoint, err)
		}
	}
}

// The spans of a task reach an OTLP/HTTP endpoint at /v1/traces.
func TestOTLPExporterSendsTheSpansOverHTTP(t *testing.T) {
	var posts atomic.Int32
	var path atomic.Value
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		posts.Add(1)
		path.Store(r.URL.Path)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	exporter, err := NewOTLPExporter(context.Background(), server.URL)
	ok(t, err)
	provider := NewProvider(exporter, true)
	h := newHarness(t)
	h.trace = NewTaskTrace(h.source, provider)
	task := approvalTask(t, h, "task-1", "corr")
	if built, err := h.trace.Build(context.Background(), task.ID()); err != nil || !built {
		t.Fatal(built, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ok(t, provider.Shutdown(ctx))
	if posts.Load() == 0 || path.Load() != "/v1/traces" {
		t.Fatalf("posts %d path %v", posts.Load(), path.Load())
	}
}

func TestExporterKeepsAPathTheEndpointGives(t *testing.T) {
	var path atomic.Value
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { path.Store(r.URL.Path) }))
	defer server.Close()
	exporter, err := NewOTLPExporter(context.Background(), server.URL+"/otlp/v1/traces")
	ok(t, err)
	provider := NewProvider(exporter, true)
	h := newHarness(t)
	h.trace = NewTaskTrace(h.source, provider)
	task := approvalTask(t, h, "task-1", "corr")
	_, err = h.trace.Build(context.Background(), task.ID())
	ok(t, err)
	ok(t, provider.Shutdown(context.Background()))
	if path.Load() != "/otlp/v1/traces" {
		t.Fatalf("path %v", path.Load())
	}
}

func TestRandomIDsWhenNoneAreAsked(t *testing.T) {
	var gen idGenerator
	traceID, spanID := gen.NewIDs(context.Background())
	if !traceID.IsValid() || !spanID.IsValid() || !gen.NewSpanID(context.Background(), traceID).IsValid() {
		t.Fatal("random ids are valid")
	}
}
