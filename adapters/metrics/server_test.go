package metrics

import (
	"context"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestServeAnswersMetricsAndHealthUntilTheContextEnds(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	registry := NewRegistry()
	NewHTTPInstrument(registry)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Serve(ctx, listener, registry) }()
	base := "http://" + listener.Addr().String()
	if body := fetch(t, base+"/metrics"); !strings.Contains(body, "go_goroutines") || !strings.Contains(body, "process_start_time_seconds") {
		t.Errorf("metrics lack the runtime:\n%s", body)
	}
	if body := fetch(t, base+"/healthz"); body != "ok\n" {
		t.Errorf("healthz = %q", body)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Serve did not stop")
	}
}

func TestServeReportsAListenerThatFails(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	_ = listener.Close()
	if err := Serve(context.Background(), listener, NewRegistry()); err == nil {
		t.Fatal("a closed listener served")
	}
}

func fetch(t *testing.T, url string) string {
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
