package axlr

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

func pinsOf(t *testing.T, versions ...string) domain.SkillPins {
	t.Helper()
	var list []domain.SkillPinSnapshot
	for _, version := range versions {
		list = append(list, domain.SkillPinSnapshot{Name: "oom", Version: version, Catalog: "axlr"})
	}
	pins, err := domain.NewSkillPins(list)
	if err != nil {
		t.Fatal(err)
	}
	return pins
}

// skilled is a fake axlr-tui that takes --skills and keeps its installs in
// a scratch AXLR state; it returns the file recording the installs.
func skilled(t *testing.T, scenario, install string) (*Executor, string, string) {
	t.Helper()
	e, args := fake(t, scenario)
	t.Setenv("FAKE_AXLR_SKILLS", "1")
	t.Setenv("FAKE_AXLR_INSTALL", install)
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	installs := filepath.Join(e.StateDir, "installs.txt")
	t.Setenv("FAKE_AXLR_INSTALLS", installs)
	return e, args, installs
}

func installLines(t *testing.T, path string) []string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSpace(string(raw)), "\n")
}

func flagValue(args []string, flag string) string {
	i := slices.Index(args, flag)
	if i < 0 || i+1 >= len(args) {
		return ""
	}
	return args[i+1]
}

func TestExecuteInstallsAMissingPinAndPassesTheSkills(t *testing.T) {
	e, args, installs := skilled(t, "result-completed", "ok")
	req := request(5 * time.Second)
	req.Skills = pinsOf(t, "1.0.0")
	report, err := e.Execute(context.Background(), req)
	if err != nil || report.Outcome != domain.ExecutionCompleted {
		t.Fatalf("report = %+v %v", report, err)
	}
	if got := installLines(t, installs); len(got) != 2 || got[0] != "list --refresh" || got[1] != "install oom@1.0.0 --from axlr --yes" {
		t.Errorf("a missing pin refreshes the index, then installs: %v", got)
	}
	file := flagValue(readArgs(t, args), "--skills")
	raw, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	var written []map[string]string
	if err := json.Unmarshal(raw, &written); err != nil {
		t.Fatal(err)
	}
	if len(written) != 1 || written[0]["name"] != "oom" || written[0]["version"] != "1.0.0" || written[0]["catalog"] != "axlr" || len(written[0]) != 3 {
		t.Errorf("skills file = %s", raw)
	}
	if report.Policy == nil || !slices.Contains(report.Policy.AllowedTools, "axlr_skill") {
		t.Errorf("the policy leaves the skill unreadable: %+v", report.Policy)
	}
	// Installed, the next run asks nothing of the network.
	req.ExecutionID = "exec-2"
	if _, err := e.Execute(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if got := installLines(t, installs); len(got) != 2 {
		t.Errorf("a pin already here reached for the network: %v", got)
	}
}

func TestASkillThatCannotBeInstalledFailsTheTaskWithItsReason(t *testing.T) {
	for install, want := range map[string]string{
		"refused":     "catalog skill axlr/oom@1.0.0 could not be installed on this node, and the task does not run without it: axlr-tui skills install: catalog axlr has no skill oom 1.0.0",
		"silent":      "exit status 1",
		"unconfirmed": "exit status 3",
	} {
		t.Run(install, func(t *testing.T) {
			e, args, _ := skilled(t, "result-completed", install)
			req := request(5 * time.Second)
			req.Skills = pinsOf(t, "1.0.0")
			report, err := e.Execute(context.Background(), req)
			if err != nil || report.Outcome != domain.ExecutionFailed || !strings.Contains(report.FailureReason, want) {
				t.Fatalf("report = %+v %v", report, err)
			}
			if !report.Result.NotStarted || report.Result.PendingDecision != nil || report.Policy == nil {
				t.Errorf("result = %+v", report.Result)
			}
			if _, err := os.Stat(args); !os.IsNotExist(err) {
				t.Error("the run started without its skill")
			}
		})
	}
}

func TestASkillInstallThatHangsIsBounded(t *testing.T) {
	e, args, _ := skilled(t, "result-completed", "hang")
	req := request(300 * time.Millisecond)
	req.Skills = pinsOf(t, "1.0.0")
	report, err := e.Execute(context.Background(), req)
	if err != nil || report.Outcome != domain.ExecutionFailed || !report.Result.NotStarted || !strings.Contains(report.FailureReason, "deadline") {
		t.Fatalf("report = %+v %v", report, err)
	}
	if _, err := os.Stat(args); !os.IsNotExist(err) {
		t.Error("the run started without its skill")
	}
}

func TestABinaryWithoutSkillsNeverRunsATaskThatNeedsThem(t *testing.T) {
	e, args := fake(t, "result-completed")
	req := request(5 * time.Second)
	req.Skills = pinsOf(t, "1.0.0")
	report, err := e.Execute(context.Background(), req)
	if err != nil || report.Outcome != domain.ExecutionFailed || !report.Result.NotStarted || !strings.Contains(report.FailureReason, "no --skills flag") {
		t.Fatalf("report = %+v %v", report, err)
	}
	if _, err := os.Stat(args); !os.IsNotExist(err) {
		t.Error("the run started")
	}
	// Without pins, an older binary runs as before and gets no flag.
	if e.SupportsSkills(context.Background()) {
		t.Error("capability")
	}
	req.Skills = domain.SkillPins{}
	if _, err := e.Execute(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if slices.Contains(readArgs(t, args), "--skills") {
		t.Error("--skills passed to a binary without it")
	}
}

// A bot with no pins gets the empty list, so what a pinned bot installed
// on this node is not in its prompt.
func TestABotWithoutPinsReadsNoInstalledSkill(t *testing.T) {
	e, args, installs := skilled(t, "result-completed", "ok")
	if !e.SupportsSkills(context.Background()) {
		t.Error("capability")
	}
	report, err := e.Execute(context.Background(), request(5*time.Second))
	if err != nil || report.Outcome != domain.ExecutionCompleted {
		t.Fatalf("report = %+v %v", report, err)
	}
	raw, err := os.ReadFile(flagValue(readArgs(t, args), "--skills"))
	if err != nil || strings.TrimSpace(string(raw)) != "[]" {
		t.Errorf("skills file = %q %v", raw, err)
	}
	if report.Policy == nil || slices.Contains(report.Policy.AllowedTools, "axlr_skill") {
		t.Errorf("an unpinned bot was given the skill tool: %+v", report.Policy)
	}
	if got := installLines(t, installs); got != nil {
		t.Errorf("installs = %v", got)
	}
}

// A decision goes on with the pins its session resolved: passing them
// again would take a bundle reinstalled while the approval waited.
func TestDecideKeepsTheSessionsPinsAndItsSkillTool(t *testing.T) {
	e, args, installs := skilled(t, "result-completed", "ok")
	decision := application.DecisionRequest{ExecutionID: "exec-2", TaskID: "task-1", BotID: "sii", SessionID: "sess-1", CallID: "c1", Decision: application.DecisionApprove, AllowedTools: []string{"local_read"}, Timeout: 5 * time.Second, Skills: pinsOf(t, "1.0.0")}
	report, err := e.Decide(context.Background(), decision)
	if err != nil || report.Outcome != domain.ExecutionCompleted {
		t.Fatalf("report = %+v %v", report, err)
	}
	if slices.Contains(readArgs(t, args), "--skills") || installLines(t, installs) != nil {
		t.Errorf("args = %v installs = %v", readArgs(t, args), installLines(t, installs))
	}
	if !slices.Contains(report.Policy.AllowedTools, "axlr_skill") {
		t.Errorf("policy = %+v", report.Policy)
	}
}

// axlr-tui's own refusal of a pin, before any request, is a run that did
// not start, for a run and for a decision.
func TestAPinRefusedByTheRuntimeDidNotStart(t *testing.T) {
	const reason = "the execution's skills are unavailable here"
	e, _, installs := skilled(t, "pin-refused", "ok")
	installed := filepath.Join(os.Getenv("XDG_STATE_HOME"), "axlr", "skills", "axlr", "oom", "1.0.0")
	if err := os.MkdirAll(installed, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(installed, "skill.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	req := request(5 * time.Second)
	req.Skills = pinsOf(t, "1.0.0")
	report, err := e.Execute(context.Background(), req)
	if err != nil || report.Outcome != domain.ExecutionFailed || !report.Result.NotStarted || !strings.Contains(report.FailureReason, reason) {
		t.Fatalf("run = %+v %v", report, err)
	}
	if got := installLines(t, installs); got != nil {
		t.Errorf("installs = %v", got)
	}
	decision := application.DecisionRequest{ExecutionID: "exec-2", TaskID: "task-1", BotID: "sii", SessionID: "sess-1", CallID: "c1", Decision: application.DecisionApprove, AllowedTools: []string{"local_read"}, Timeout: 5 * time.Second, Skills: pinsOf(t, "1.0.0")}
	report, err = e.Decide(context.Background(), decision)
	if err != nil || report.Outcome != domain.ExecutionFailed || !report.Result.NotStarted || !strings.Contains(report.FailureReason, reason) {
		t.Fatalf("decision = %+v %v", report, err)
	}
}

func TestARunThatStartedIsNeverNotStarted(t *testing.T) {
	e, _ := fake(t, "pin-refused-late")
	report, err := e.Execute(context.Background(), request(5*time.Second))
	if err != nil || report.Result.NotStarted {
		t.Fatalf("report = %+v %v", report, err)
	}
	for _, c := range []struct {
		code, lines int
		line        string
		want        bool
	}{
		{exitFailed, 0, "axlr-tui: the execution's skills are unavailable here: x", true},
		{exitFailed, 1, "axlr-tui: the execution's skills are unavailable here: x", false},
		{exitNeedsDecision, 0, "axlr-tui: the execution's skills are unavailable here: x", false},
		{exitFailed, 0, "axlr-tui: no model provider", false},
	} {
		if got := pinRefused(c.code, c.lines, c.line); got != c.want {
			t.Errorf("%+v = %v", c, got)
		}
	}
}

// A refresh that fails changes nothing: the install still answers.
func TestARefreshThatFailsLeavesTheInstallToAnswer(t *testing.T) {
	e, _, installs := skilled(t, "result-completed", "refresh-fails")
	req := request(5 * time.Second)
	req.Skills = pinsOf(t, "1.0.0")
	report, err := e.Execute(context.Background(), req)
	if err != nil || report.Outcome != domain.ExecutionCompleted {
		t.Fatalf("report = %+v %v", report, err)
	}
	if got := installLines(t, installs); len(got) != 2 {
		t.Errorf("installs = %v", got)
	}
}

// A waiter stops at its cancel, not at the end of the other's install.
func TestACancelledWaiterStopsWaitingForTheInstall(t *testing.T) {
	e, args, installs := skilled(t, "result-completed", "hang")
	e.WaitDelay = 100 * time.Millisecond
	req := func(id domain.ExecutionID) application.ExecutionRequest {
		r := request(4 * time.Second)
		r.ExecutionID, r.Skills = id, pinsOf(t, "1.0.0")
		return r
	}
	holder := make(chan struct{})
	go func() {
		defer close(holder)
		_, _ = e.Execute(context.Background(), req("exec-holder"))
	}()
	t.Cleanup(func() { <-holder })
	for deadline := time.Now().Add(3 * time.Second); len(installLines(t, installs)) < 2; {
		if time.Now().After(deadline) {
			t.Fatal("the holder never started its install")
		}
		time.Sleep(10 * time.Millisecond)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan application.ExecutionReport, 1)
	go func() {
		report, _ := e.Execute(ctx, req("exec-waiter"))
		done <- report
	}()
	time.Sleep(100 * time.Millisecond)
	cancelled := time.Now()
	cancel()
	select {
	case report := <-done:
		if waited := time.Since(cancelled); waited > time.Second {
			t.Errorf("it waited %s after its cancel", waited)
		}
		if !report.Result.NotStarted || !strings.Contains(report.FailureReason, "stopped waiting") {
			t.Errorf("report = %+v", report)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the waiter did not stop")
	}
	if _, err := os.Stat(args); !os.IsNotExist(err) {
		t.Error("a run started")
	}
}

// What installing took comes out of the run's time.
func TestAfterInstallDeductsWhatInstallingTook(t *testing.T) {
	e := NewExecutor("x", t.TempDir())
	if got, err := e.afterInstall(time.Minute, 0); err != nil || got != time.Minute {
		t.Errorf("nothing installed: %v %v", got, err)
	}
	if got, err := e.afterInstall(time.Minute, 20*time.Second); err != nil || got != 40*time.Second {
		t.Errorf("deducted: %v %v", got, err)
	}
	if got, err := e.afterInstall(0, time.Minute); err != nil || got != defaultTimeout-time.Minute {
		t.Errorf("default: %v %v", got, err)
	}
	if _, err := e.afterInstall(time.Minute, time.Minute); err == nil || !strings.Contains(err.Error(), "all of its timeout") {
		t.Errorf("spent: %v", err)
	}
}

func TestWithSkillTool(t *testing.T) {
	pins := pinsOf(t, "1.0.0")
	if got := withSkillTool([]string{"local_read"}, pins); !slices.Equal(got, []string{"local_read", "axlr_skill"}) {
		t.Errorf("added = %v", got)
	}
	if got := withSkillTool([]string{"axlr_skill"}, pins); len(got) != 1 {
		t.Errorf("twice = %v", got)
	}
	if got := withSkillTool(nil, pins); got != nil {
		t.Errorf("an empty list means the level decides: %v", got)
	}
	if got := withSkillTool([]string{"local_read"}, domain.SkillPins{}); len(got) != 1 {
		t.Errorf("no pins = %v", got)
	}
}

func TestAXLRStateFollowsTheBinarysResolution(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "/x/state")
	if got := axlrState(); got != "/x/state/axlr" {
		t.Errorf("xdg = %q", got)
	}
	t.Setenv("XDG_STATE_HOME", "relative")
	t.Setenv("HOME", "/home/u")
	if got := axlrState(); got != "/home/u/.local/state/axlr" {
		t.Errorf("home = %q", got)
	}
	t.Setenv("HOME", "")
	t.Setenv("XDG_STATE_HOME", "")
	if got := axlrState(); got != "" {
		t.Errorf("none = %q", got)
	}
	pin, _ := domain.NewSkillPin(domain.SkillPinSnapshot{Name: "oom", Version: "1.0.0", Catalog: "axlr"})
	if skillInstalled(pin) {
		t.Error("installed with no state to look in")
	}
}
