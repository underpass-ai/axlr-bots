package main

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestNodeVersionRelease(t *testing.T) {
	const version = "0.4.1"
	const pkg = "github.com/underpass-ai/axlr-bots/internal/buildinfo"
	binary := filepath.Join(t.TempDir(), "axlr-node")
	flags := "-s -w -X " + pkg + ".Version=" + version + " -X " + pkg + ".Revision=test-revision -X " + pkg + ".Time=2026-11-13T00:00:00Z"
	build := exec.Command("go", "build", "-ldflags="+flags, "-o", binary, ".")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build release binary: %v\n%s", err, output)
	}
	for _, arg := range []string{"version", "--version"} {
		t.Run(arg, func(t *testing.T) {
			output, err := exec.Command(binary, arg).CombinedOutput()
			if err != nil {
				t.Fatalf("version query: %v\n%s", err, output)
			}
			if got, want := strings.TrimSpace(string(output)), "axlr-node "+version; got != want {
				t.Errorf("output = %q, want %q", got, want)
			}
		})
	}
}
