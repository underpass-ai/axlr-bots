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
	if got := installLines(t, installs); len(got) != 1 || got[0] != "install oom@1.0.0 --from axlr --yes" {
		t.Errorf("installs = %v", got)
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
	if got := installLines(t, installs); len(got) != 1 {
		t.Errorf("reinstalled: %v", got)
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

func TestDecideInstallsAndPassesTheSkills(t *testing.T) {
	e, args, installs := skilled(t, "result-completed", "ok")
	decision := application.DecisionRequest{ExecutionID: "exec-2", TaskID: "task-1", BotID: "sii", SessionID: "sess-1", CallID: "c1", Decision: application.DecisionApprove, AllowedTools: []string{"local_read"}, Timeout: 5 * time.Second, Skills: pinsOf(t, "1.0.0")}
	report, err := e.Decide(context.Background(), decision)
	if err != nil || report.Outcome != domain.ExecutionCompleted {
		t.Fatalf("report = %+v %v", report, err)
	}
	if flagValue(readArgs(t, args), "--skills") == "" || len(installLines(t, installs)) != 1 || !slices.Contains(report.Policy.AllowedTools, "axlr_skill") {
		t.Errorf("args = %v", readArgs(t, args))
	}
}

func TestDecideRefusedWithoutItsSkill(t *testing.T) {
	e, args, _ := skilled(t, "result-completed", "refused")
	decision := application.DecisionRequest{ExecutionID: "exec-2", TaskID: "task-1", BotID: "sii", SessionID: "sess-1", CallID: "c1", Decision: application.DecisionApprove, AllowedTools: []string{"local_read"}, Timeout: 5 * time.Second, Skills: pinsOf(t, "1.0.0")}
	report, err := e.Decide(context.Background(), decision)
	if err != nil || report.Outcome != domain.ExecutionFailed || !report.Result.NotStarted {
		t.Fatalf("report = %+v %v", report, err)
	}
	if _, err := os.Stat(args); !os.IsNotExist(err) {
		t.Error("the decision ran without its skill")
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
