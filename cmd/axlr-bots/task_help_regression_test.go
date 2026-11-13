package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

func TestTaskHelp(t *testing.T) {
	for _, help := range []string{"--help", "-h"} {
		t.Run(help, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			state := filepath.Join(t.TempDir(), "missing", "state")
			code := run([]string{"--state", state, "task", help}, &stdout, &stderr)
			if code != exitOK || stderr.Len() != 0 {
				t.Fatalf("task help: exit %d, stderr %q", code, stderr.String())
			}
			for _, want := range []string{"usage: axlr-bots task", "task submit", "task list", "task activity", "task reconcile"} {
				if !strings.Contains(stdout.String(), want) {
					t.Errorf("task help missing %q: %s", want, stdout.String())
				}
			}
			if strings.Contains(stdout.String(), "unknown command") || strings.Contains(stdout.String(), "bot register") {
				t.Errorf("task help includes unrelated output: %s", stdout.String())
			}
		})
	}
}
