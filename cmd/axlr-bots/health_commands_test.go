package main

import (
	"bytes"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/underpass-ai/axlr-bots/domain"
)

func TestHealthAnswersWhatTheSocketSays(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "cp.sock")
	var stdout, stderr bytes.Buffer
	if code := run([]string{"--socket", socket, "health"}, &stdout, &stderr); code != exitError || !strings.Contains(stderr.String(), "no service answers") {
		t.Fatalf("nothing listening: exit %d, stderr %q", code, stderr.String())
	}
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/healthz" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { _ = server.Close() })
	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"--socket", socket, "health"}, &stdout, &stderr); code != exitOK || strings.TrimSpace(stdout.String()) != "ok" {
		t.Fatalf("a daemon answering: exit %d, stdout %q, stderr %q", code, stdout.String(), stderr.String())
	}
}

func TestHealthUsage(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"--socket", "x.sock", "health", "extra"}, &stdout, &stderr); code != exitUsage {
		t.Fatalf("an argument: exit %d", code)
	}
	if code := health(t.Context(), "", remoteControl{}, nil, &stdout, &stderr); code != exitUsage || !strings.Contains(stderr.String(), "needs a socket") {
		t.Fatalf("no socket: exit %d, stderr %q", code, stderr.String())
	}
}

func TestVersionPrintsTheBuild(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"version"}, &stdout, &stderr); code != exitOK || !strings.HasPrefix(stdout.String(), "axlr-bots ") {
		t.Fatalf("exit %d, stdout %q", code, stdout.String())
	}
	if code := run([]string{"version", "extra"}, &stdout, &stderr); code != exitUsage {
		t.Fatalf("an argument: exit %d", code)
	}
}

func TestDescribeVersion(t *testing.T) {
	cases := map[string]domain.BuildIdentity{
		"axlr-bots (development build)":                        {},
		"axlr-bots (development build) (abc)":                  {Version: "(devel)", Revision: "abc"},
		"axlr-bots 0.4.0 (abc 2026-11-12T10:00:00Z)":           {Version: "0.4.0", Revision: "abc", Time: "2026-11-12T10:00:00Z"},
		"axlr-bots v0.4.1-0.2026-abc+dirty (abc with changes)": {Version: "v0.4.1-0.2026-abc+dirty", Revision: "abc", Modified: true},
	}
	for want, build := range cases {
		if got := describeVersion(build); got != want {
			t.Errorf("describeVersion(%+v) = %q, want %q", build, got, want)
		}
	}
}
