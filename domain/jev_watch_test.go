package domain

import (
	"strings"
	"testing"
)

func watcherSnapshot() *RuntimeSnapshot {
	return &RuntimeSnapshot{Kind: "jev", Jev: &JevProgramSnapshot{
		Server: WatchServer, Start: WatchNextTool.String(), Wait: WatchNextTool.String(),
		Act: JevActSnapshot{Tool: WatchDecideTool.String(), Choice: WatchChoice, Verdict: WatchVerdict},
	}}
}

// A program on the reserved server axlr-bots is the watcher's, exactly:
// its two tools in their places, Jev's choice and verdict by their names,
// no stop tool.
func TestTheReservedServerTakesOnlyTheWatchersProgram(t *testing.T) {
	runtime, err := NewRuntime(watcherSnapshot())
	if err != nil {
		t.Fatal(err)
	}
	if program, _ := runtime.Jev(); !program.WatchesQuestions() {
		t.Error("the watcher's program does not watch")
	}
	cases := map[string]func(*JevProgramSnapshot){
		"another start":   func(s *JevProgramSnapshot) { s.Start = "batches" },
		"another wait":    func(s *JevProgramSnapshot) { s.Wait = "listen" },
		"another act":     func(s *JevProgramSnapshot) { s.Act.Tool = "process" },
		"a stop":          func(s *JevProgramSnapshot) { s.Stop = "stop" },
		"another choice":  func(s *JevProgramSnapshot) { s.Act.Choice = "decision" },
		"no verdict":      func(s *JevProgramSnapshot) { s.Act.Verdict = "" },
		"another verdict": func(s *JevProgramSnapshot) { s.Act.Verdict = "judge" },
	}
	for name, change := range cases {
		s := watcherSnapshot()
		change(s.Jev)
		if _, err := NewRuntime(s); err == nil || !strings.Contains(err.Error(), "axlr-bots") {
			t.Errorf("%s: %v", name, err)
		}
	}
	// Another server's program never watches, whatever its tools.
	other := watcherSnapshot()
	other.Jev.Server = "queue"
	runtime, err = NewRuntime(other)
	if err != nil {
		t.Fatal(err)
	}
	if program, _ := runtime.Jev(); program.WatchesQuestions() {
		t.Error("a program of another server watches")
	}
}

// A bot watches its team's questions when it runs the watcher's program;
// its allowed tools and level must carry it, as for any Jev program.
func TestABotWatchesWhenItRunsTheWatchersProgram(t *testing.T) {
	model, _ := NewModelPolicy("", "", false, 0)
	budgets, _ := NewBudgets(0, 0, 0, 0)
	tools, _ := NewAllowedTools([]string{WatchNextTool.String()})
	bot, err := NewBot("qa-vigia", "default", "QA watcher", "operator", "watch", tools, model, budgets, t0)
	if err != nil {
		t.Fatal(err)
	}
	if bot.WatchesQuestions() {
		t.Error("an AXLR bot watches")
	}
	if err := bot.Govern(AutonomyActWithinPolicy, ToolRules{}); err != nil {
		t.Fatal(err)
	}
	watcher, _ := NewRuntime(watcherSnapshot())
	if err := bot.SetRuntime(watcher); err == nil || !strings.Contains(err.Error(), WatchDecideTool.String()) {
		t.Errorf("without the decide tool: %v", err)
	}
	both, _ := NewAllowedTools([]string{WatchNextTool.String(), WatchDecideTool.String()})
	if err := bot.Redefine(bot.Mission(), both, model, budgets); err != nil {
		t.Fatal(err)
	}
	if err := bot.SetRuntime(watcher); err != nil || !bot.WatchesQuestions() {
		t.Errorf("the watcher: %v %v", err, bot.WatchesQuestions())
	}
}
