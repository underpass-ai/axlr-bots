package fakeaxlr

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestScenarios(t *testing.T) {
	for _, scenario := range []string{"completed", "slow", "decision", "failed", "approval-flow", "delivered"} {
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
		if version, err := exec.Command(bin, "--version").Output(); err != nil || strings.TrimSpace(string(version)) != "axlr-tui 0.0.0-fake" {
			t.Errorf("%s: version = %q %v", scenario, version, err)
		}
		run := exec.Command(bin, "--exec", "hi", "--json")
		run.Dir = dir
		t.Chdir(dir)
		out, runErr := run.CombinedOutput()
		args, _ := os.ReadFile(argsFile)
		if !strings.HasPrefix(string(args), "--exec\nhi\n") {
			t.Errorf("%s: args = %q", scenario, args)
		}
		code := 0
		if exitErr, ok := runErr.(*exec.ExitError); ok {
			code = exitErr.ExitCode()
		}
		switch scenario {
		case "completed", "slow":
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
		case "delivered":
			notes, _ := os.ReadFile(filepath.Join(".axlr", "deliverables", "sess-1", "notes.md"))
			if code != 0 || !strings.Contains(string(out), `"type":"receipt"`) || !strings.Contains(string(out), `"name":"plan.md"`) || string(notes) != DeliveredContent {
				t.Errorf("delivered: %d %s %q", code, out, notes)
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
