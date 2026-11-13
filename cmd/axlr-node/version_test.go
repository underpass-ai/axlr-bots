package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/underpass-ai/axlr-bots/internal/buildinfo"
)

func TestNodeVersion(t *testing.T) {
	old := buildinfo.Version
	buildinfo.Version = "0.4.1"
	t.Cleanup(func() { buildinfo.Version = old })

	for _, arg := range []string{"version", "--version"} {
		t.Run(arg, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if code := run([]string{arg}, &stdout, &stderr); code != 0 {
				t.Fatalf("exit = %d, stderr = %q", code, stderr.String())
			}
			if !strings.Contains(stdout.String(), "0.4.1") {
				t.Errorf("version output = %q, want release version", stdout.String())
			}
		})
	}
}
