package fakeaxlr

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestScenarios(t *testing.T) {
	for _, scenario := range []string{"completed", "decision", "failed", "approval-flow"} {
		dir := t.TempDir()
		marker := filepath.Join(dir, "marker")
		bin, argsFile, err := Write(dir, Options{Scenario: scenario, Marker: marker})
		if err != nil {
			t.Fatal(err)
		}
		help, _ := exec.Command(bin, "--help").CombinedOutput()
		if !strings.Contains(string(help), "-policy") {
			t.Errorf("%s: help = %s", scenario, help)
		}
		out, runErr := exec.Command(bin, "--exec", "hi", "--json").CombinedOutput()
		args, _ := os.ReadFile(argsFile)
		if !strings.HasPrefix(string(args), "--exec\nhi\n") {
			t.Errorf("%s: args = %q", scenario, args)
		}
		code := 0
		if exitErr, ok := runErr.(*exec.ExitError); ok {
			code = exitErr.ExitCode()
		}
		switch scenario {
		case "completed":
			if code != 0 || !strings.Contains(string(out), `"status":"completed"`) {
				t.Errorf("completed: %d %s", code, out)
			}
		case "decision", "approval-flow":
			if code != 3 || !strings.Contains(string(out), "decision.required") {
				t.Errorf("%s: %d %s", scenario, code, out)
			}
			if _, err := os.Stat(marker); err == nil {
				t.Errorf("%s: the write ran without approval", scenario)
			}
		case "failed":
			if code != 1 {
				t.Errorf("failed: %d", code)
			}
		}
		if scenario == "approval-flow" {
			out, _ := exec.Command(bin, "--session", "sess-1", "--decide", "deny", "--json").CombinedOutput()
			if _, err := os.Stat(marker); err == nil || !strings.Contains(string(out), "denied") {
				t.Errorf("deny ran the write or did not complete: %s", out)
			}
			out, _ = exec.Command(bin, "--session", "sess-1", "--decide", "approve", "--json").CombinedOutput()
			if _, err := os.Stat(marker); err != nil || !strings.Contains(string(out), `"status":"completed"`) {
				t.Errorf("approve did not run the write: %s", out)
			}
		}
	}
	dir := t.TempDir()
	bin, _, _ := Write(dir, Options{NoPolicy: true})
	help, _ := exec.Command(bin, "--help").CombinedOutput()
	if strings.Contains(string(help), "-policy") || !strings.Contains(string(help), "-exec") {
		t.Errorf("no-policy help = %s", help)
	}
	if _, _, err := Write(filepath.Join(dir, "missing", "dir"), Options{}); err == nil {
		t.Error("write under a missing dir succeeded")
	}
}
