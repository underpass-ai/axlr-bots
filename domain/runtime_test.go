package domain

import (
	"strings"
	"testing"
)

func jevSnapshot() *RuntimeSnapshot {
	return &RuntimeSnapshot{Kind: "jev", Jev: &JevProgramSnapshot{
		Server: "queue", Start: "batches", Wait: "listen", Stop: "stop",
		Act: JevActSnapshot{Tool: "process", Choice: "batch", Verdict: "judge", Pass: []string{"priority"}},
	}}
}

// A runtime is AXLR unless it names a Jev program; the program is
// validated and read back as it was written, with the model pinned.
func TestRuntimeIsAXLRUnlessItNamesAJevProgram(t *testing.T) {
	none, err := NewRuntime(nil)
	if err != nil || none.Kind() != RuntimeAXLR || none.Snapshot() != nil {
		t.Fatalf("nil = %+v %v", none, err)
	}
	if _, ok := none.Jev(); ok {
		t.Error("AXLR has a program")
	}
	axlr, err := NewRuntime(&RuntimeSnapshot{Kind: "axlr"})
	if err != nil || axlr.Kind() != RuntimeAXLR || axlr.Snapshot() != nil {
		t.Errorf("axlr = %+v %v", axlr, err)
	}
	jev, err := NewRuntime(jevSnapshot())
	if err != nil {
		t.Fatal(err)
	}
	program, ok := jev.Jev()
	if !ok || jev.Kind() != RuntimeJev || program.Server() != "queue" || program.Start() != "batches" || program.Act() != "process" || program.Wait() != "listen" || program.Stop() != "stop" || program.Choice() != "batch" || program.Verdict() != "judge" || program.Model() != DefaultJevModel {
		t.Errorf("program = %+v", program)
	}
	if tools := program.Tools(); len(tools) != 4 || tools[3] != "stop" {
		t.Errorf("tools = %v", tools)
	}
	back := jev.Snapshot()
	want := jevSnapshot()
	want.Jev.Model = DefaultJevModel
	if back.Kind != "jev" || back.Jev.Server != want.Jev.Server || back.Jev.Start != want.Jev.Start || back.Jev.Wait != want.Jev.Wait || back.Jev.Stop != want.Jev.Stop || back.Jev.Model != want.Jev.Model || back.Jev.Act.Tool != want.Jev.Act.Tool || back.Jev.Act.Choice != want.Jev.Act.Choice || back.Jev.Act.Verdict != want.Jev.Act.Verdict || len(back.Jev.Act.Pass) != 1 || back.Jev.Act.Pass[0] != "priority" {
		t.Errorf("snapshot = %+v", back)
	}
	for _, kind := range []string{"", "axlr", "jev"} {
		if _, err := ParseRuntimeKind(kind); err != nil {
			t.Error(kind, err)
		}
	}
	if _, err := ParseRuntimeKind("haiku"); err == nil {
		t.Error("haiku is a runtime")
	}
}

func TestAJevProgramIsValidated(t *testing.T) {
	cases := map[string]func(*RuntimeSnapshot){
		"axlr with a program": func(s *RuntimeSnapshot) { s.Kind = "axlr" },
		"jev without program": func(s *RuntimeSnapshot) { s.Jev = nil },
		"unknown kind":        func(s *RuntimeSnapshot) { s.Kind = "llm" },
		"no server":           func(s *RuntimeSnapshot) { s.Jev.Server = "" },
		"bad server":          func(s *RuntimeSnapshot) { s.Jev.Server = "a b" },
		"no start":            func(s *RuntimeSnapshot) { s.Jev.Start = "" },
		"bad act tool":        func(s *RuntimeSnapshot) { s.Jev.Act.Tool = "Process" },
		"no wait":             func(s *RuntimeSnapshot) { s.Jev.Wait = "" },
		"bad stop":            func(s *RuntimeSnapshot) { s.Jev.Stop = "a b" },
		"no choice argument":  func(s *RuntimeSnapshot) { s.Jev.Act.Choice = "" },
		"verdict is choice":   func(s *RuntimeSnapshot) { s.Jev.Act.Verdict = "batch" },
		"pass is choice":      func(s *RuntimeSnapshot) { s.Jev.Act.Pass = []string{"batch"} },
		"pass twice":          func(s *RuntimeSnapshot) { s.Jev.Act.Pass = []string{"a", "a"} },
		"too many pass":       func(s *RuntimeSnapshot) { s.Jev.Act.Pass = strings.Split("a b c d e f g h i", " ") },
		"model not pinned":    func(s *RuntimeSnapshot) { s.Jev.Model = "jev-latest" },
		"model with spaces":   func(s *RuntimeSnapshot) { s.Jev.Model = "jev 1" },
		"bad argument name":   func(s *RuntimeSnapshot) { s.Jev.Act.Choice = "1x" },
		"bad pass field":      func(s *RuntimeSnapshot) { s.Jev.Act.Pass = []string{"pri-ority"} },
	}
	for name, change := range cases {
		s := jevSnapshot()
		change(s)
		if _, err := NewRuntime(s); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	for name, tell := range map[string]*JevTellSnapshot{
		"no name":         {Name: ""},
		"not a bot's":     {Name: "error.worker"},
		"unknown field":   {Name: "info.worker.processed", Fields: []string{"speed"}},
		"field twice":     {Name: "info.worker.processed", Fields: []string{"choice", "choice"}},
		"the verdict arg": {Name: "info.worker.processed", Fields: []string{"judge"}},
	} {
		s := jevSnapshot()
		s.Jev.Tell = tell
		if _, err := NewRuntime(s); err == nil {
			t.Errorf("tell %s: accepted", name)
		}
	}
	telling := jevSnapshot()
	telling.Jev.Tell = &JevTellSnapshot{Name: "info.worker.processed", Fields: []string{"batch", "probability", "priority"}}
	told, err := NewRuntime(telling)
	if err != nil {
		t.Fatal(err)
	}
	if program, _ := told.Jev(); true {
		name, fields, ok := program.Tell()
		if !ok || name != "info.worker.processed" || len(fields) != 3 || told.Snapshot().Jev.Tell == nil || told.Snapshot().Jev.Tell.Fields[2] != "priority" {
			t.Errorf("tell = %v %v %v", name, fields, ok)
		}
	}
	minimal := &RuntimeSnapshot{Kind: "jev", Jev: &JevProgramSnapshot{Server: "w", Start: "a", Wait: "c", Act: JevActSnapshot{Tool: "b", Choice: "x"}}}
	runtime, err := NewRuntime(minimal)
	if err != nil {
		t.Fatal(err)
	}
	if program, _ := runtime.Jev(); program.Stop() != "" || program.Verdict() != "" || len(program.Pass()) != 0 || len(program.Tools()) != 3 {
		t.Errorf("minimal = %+v", program)
	}
}

// A Jev program fits a bot only when its tools are allowed and the bot
// acts within the policy; a bot that narrows them afterwards is refused.
func TestAJevProgramMustFitTheBotsToolsAndLevel(t *testing.T) {
	tools, _ := NewAllowedTools([]string{"batches", "process", "listen", "stop"})
	model, _ := NewModelPolicy("", "", false, 0)
	budgets, _ := NewBudgets(0, 0, 0, 0)
	bot, err := NewBot("worker-1", "default", "Worker 1", "operator", "process", tools, model, budgets, t0)
	if err != nil {
		t.Fatal(err)
	}
	jev, _ := NewRuntime(jevSnapshot())
	if err := bot.SetRuntime(jev); err == nil || !strings.Contains(err.Error(), "act-within-policy") {
		t.Errorf("observe with a program: %v", err)
	}
	if err := bot.Govern(AutonomyActWithinPolicy, ToolRules{}); err != nil {
		t.Fatal(err)
	}
	if err := bot.SetRuntime(jev); err != nil || bot.Runtime().Kind() != RuntimeJev {
		t.Fatalf("set: %v", err)
	}
	fewer, _ := NewAllowedTools([]string{"batches", "process"})
	if err := bot.Redefine(bot.Mission(), fewer, model, budgets); err != nil {
		t.Fatal(err)
	}
	if err := bot.CheckRuntime(); err == nil || !strings.Contains(err.Error(), "listen") {
		t.Errorf("narrowed tools: %v", err)
	}
	snapshot := bot.Snapshot()
	if snapshot.Runtime == nil || snapshot.Runtime.Kind != "jev" {
		t.Errorf("snapshot runtime = %+v", snapshot.Runtime)
	}
	snapshot.AllowedTools = []string{"batches", "process", "listen", "stop"}
	snapshot.UpdatedAt = t0
	restored, err := RestoreBot(snapshot)
	if err != nil || restored.Runtime().Kind() != RuntimeJev {
		t.Errorf("restored = %v %v", restored.Runtime().Kind(), err)
	}
	snapshot.Level = "observe"
	if _, err := RestoreBot(snapshot); err == nil {
		t.Error("a stored bot whose level no longer fits its program was restored")
	}
	if !(NodeCapabilities{}).Runs(RuntimeAXLR) || (NodeCapabilities{}).Runs(RuntimeJev) || !(NodeCapabilities{Runtimes: []string{"jev"}}).Runs(RuntimeJev) {
		t.Error("node capabilities")
	}
}
