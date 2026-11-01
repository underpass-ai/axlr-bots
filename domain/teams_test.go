package domain

import (
	"strings"
	"testing"
	"time"
)

func TestTeams(t *testing.T) {
	teams, err := NewTeams([]string{"ops", "infra"})
	if err != nil {
		t.Fatal(err)
	}
	if got := teams.Snapshot(); len(got) != 2 || got[0] != "infra" || got[1] != "ops" {
		t.Errorf("snapshot = %v", got)
	}
	if !teams.Has("ops") || teams.Has("sre") || teams.Empty() {
		t.Errorf("teams = %+v", teams)
	}
	other, _ := NewTeams([]string{"ops", "sre"})
	if shared := teams.Shared(other); len(shared) != 1 || shared[0] != "ops" {
		t.Errorf("shared = %v", shared)
	}
	if none, _ := NewTeams(nil); !none.Empty() || none.Snapshot() != nil {
		t.Errorf("no teams = %+v", none)
	}
	for _, bad := range [][]string{{"Ops"}, {"a", "a"}, {"a", "b", "c", "d", "e", "f", "g", "h", "i"}, {""}} {
		if _, err := NewTeams(bad); err == nil {
			t.Errorf("%v accepted", bad)
		}
	}
	names := teams.Names()
	names[0] = "changed"
	if teams.Names()[0] != "infra" {
		t.Error("Names shares its slice")
	}
}

func TestMissionPurpose(t *testing.T) {
	cases := map[string]string{
		"Watch service-1 at dawn. Then report.":  "Watch service-1 at dawn.",
		"Watch the queue!  Report.":              "Watch the queue!",
		"Is the gate open? Say so.":              "Is the gate open?",
		"Read the files of the workspace":        "Read the files of the workspace",
		"Version 0.15 is out.\nTell the others.": "Version 0.15 is out.",
	}
	for mission, want := range cases {
		if got := Mission(mission).Purpose(); got != want {
			t.Errorf("%q: purpose %q, want %q", mission, got, want)
		}
	}
	long := Mission(strings.Repeat("word ", 60))
	if got := long.Purpose(); len([]rune(got)) != MaxPurposeLength || !strings.HasSuffix(got, "…") {
		t.Errorf("long purpose = %q (%d runes)", got, len([]rune(got)))
	}
}

func TestPlacementWhere(t *testing.T) {
	nodes, _ := NewPlacement([]string{"worker-1", "worker-2"}, nil)
	labels, _ := NewPlacement(nil, map[string]string{"zone": "eu", "gpu": "yes"})
	cases := map[string]Placement{"node worker-1, worker-2": nodes, "nodes labelled gpu=yes, zone=eu": labels, "any node": {}}
	for want, p := range cases {
		if got := p.Where(); got != want {
			t.Errorf("where = %q, want %q", got, want)
		}
	}
}

func teamTestBot(t *testing.T, id, tenant string, teams []string) Bot {
	t.Helper()
	bot, err := NewBot(BotID(id), TenantID(tenant), BotName(id), "operator", Mission("You are "+id+". You watch."), AllowedTools{}, ModelPolicy{}, Budgets{}, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	joined, err := NewTeams(teams)
	if err != nil {
		t.Fatal(err)
	}
	bot.JoinTeams(joined)
	return bot
}

func TestTeammatesAndTheirConsent(t *testing.T) {
	watcher := teamTestBot(t, "watcher", "default", []string{"ops"})
	worker := teamTestBot(t, "worker-1", "default", []string{"ops", "sre"})
	stranger := teamTestBot(t, "stranger", "default", []string{"infra"})
	elsewhere := teamTestBot(t, "elsewhere", "staging", []string{"ops"})
	if !watcher.IsTeammateOf(worker) || watcher.IsTeammateOf(watcher) || watcher.IsTeammateOf(stranger) || watcher.IsTeammateOf(elsewhere) {
		t.Error("teammates are wrong")
	}
	if !worker.TakesDelegationFrom(watcher) || stranger.TakesDelegationFrom(watcher) {
		t.Error("consent through the team is wrong")
	}
	refuse := false
	delegation, err := NewDelegation(DelegationSnapshot{FromTeam: &refuse})
	if err != nil {
		t.Fatal(err)
	}
	if err := worker.SetDelegation(delegation); err != nil {
		t.Fatal(err)
	}
	if worker.TakesDelegationFrom(watcher) {
		t.Error("fromTeam false still takes the team's tasks")
	}
	triggers, _ := NewTriggers([]TriggerSnapshot{{Type: "manual"}, {Type: "delegation"}})
	worker.Wire(triggers, Placement{})
	if !worker.TakesDelegationFrom(watcher) || !worker.TakesDelegationFrom(stranger) {
		t.Error("the delegation trigger is consent for anyone")
	}
	if s := worker.Delegation().Snapshot(); s == nil || s.FromTeam == nil || *s.FromTeam {
		t.Errorf("snapshot = %+v", s)
	}
	if s := (Delegation{}).Snapshot(); s != nil {
		t.Errorf("empty snapshot = %+v", s)
	}
	mate := TeammateOf(watcher, worker)
	if mate.ID != "worker-1" || len(mate.Teams) != 1 || mate.Purpose != "You are worker-1." || mate.Where != "any node" || !mate.TakesDelegation {
		t.Errorf("teammate = %+v", mate)
	}
}

func TestBotSnapshotKeepsTheTeams(t *testing.T) {
	bot := teamTestBot(t, "watcher", "default", []string{"ops"})
	bot.Register(time.Unix(2, 0))
	s := bot.Snapshot()
	if len(s.Groups) != 1 || s.Groups[0] != "ops" {
		t.Fatalf("groups = %v", s.Groups)
	}
	s.Model = ModelPolicySnapshot{Model: "m", RoutingPolicy: "cost-optimized"}
	s.Budgets = BudgetsSnapshot{MaxModelCallsPerTask: 1, MaxToolCallsPerTask: 1, TaskTimeoutSeconds: 60}
	s.Level = "observe"
	restored, err := RestoreBot(s)
	if err != nil || !restored.Teams().Has("ops") {
		t.Fatalf("restored = %+v %v", restored.Snapshot().Groups, err)
	}
	s.Groups = []string{"Bad"}
	if _, err := RestoreBot(s); err == nil {
		t.Error("a bad team was restored")
	}
}

func TestTeamDirectoryRenders(t *testing.T) {
	mates := []Teammate{
		{ID: "worker-1", Teams: []TeamName{"ops"}, Purpose: "Deploys service-1.", Tools: []string{"run_job"}, Where: "node worker-1", TakesDelegation: true},
		{ID: "worker-2", Teams: []TeamName{"ops", "sre"}, Purpose: "Deploys service-2.", Where: "any node"},
	}
	one := NewTeamDirectory([]TeamName{"ops"}, mates)
	text := one.Render(4000)
	want := "Your teammates (team ops), bots of your operator that work with you:\n- worker-1: Deploys service-1. Tools: run_job. Runs on node worker-1. Takes delegated tasks.\n- worker-2: Deploys service-2. Runs on any node."
	if text != want {
		t.Errorf("render =\n%s\nwant\n%s", text, want)
	}
	many := NewTeamDirectory([]TeamName{"ops", "sre"}, mates)
	if text := many.Render(4000); !strings.Contains(text, "(teams ops, sre)") || !strings.Contains(text, "- worker-2 (ops, sre): ") {
		t.Errorf("many teams =\n%s", text)
	}
	heading := len("Your teammates (team ops), bots of your operator that work with you:\n")
	cut := one.Render(heading + len("… and 2 more\n"))
	if cut != "Your teammates (team ops), bots of your operator that work with you:\n… and 2 more" {
		t.Errorf("cut = %q", cut)
	}
	if got := one.Render(10); got != "" {
		t.Errorf("too small = %q", got)
	}
	if got := NewTeamDirectory(nil, nil).Render(4000); got != "" || !NewTeamDirectory(nil, nil).Empty() {
		t.Errorf("empty = %q", got)
	}
	if len(one.Members()) != 2 {
		t.Error("members lost")
	}
	var lots []Teammate
	for i := 0; i < 40; i++ {
		lots = append(lots, Teammate{ID: BotID("bot-" + string(rune('a'+i%26)) + string(rune('a'+i/26))), Purpose: "x.", Where: "any node"})
	}
	if text := NewTeamDirectory([]TeamName{"ops"}, lots).Render(8000); strings.Count(text, "\n- ") != MaxDirectoryMembers || !strings.HasSuffix(text, "… and 8 more") {
		t.Errorf("capped =\n%s", text)
	}
}

func TestTeamPromptFitsTheLimit(t *testing.T) {
	mission, _ := NewMission("Watch the services.")
	objective, _ := NewObjective("Warn worker-1.")
	plain, _ := NewPrompt(mission, objective)
	if got, err := NewTeamPrompt(mission, objective, TeamDirectory{}, EventNotes{}); err != nil || got != plain {
		t.Errorf("no directory = %q %v", got, err)
	}
	directory := NewTeamDirectory([]TeamName{"ops"}, []Teammate{{ID: "worker-1", Purpose: "Deployer.", Where: "any node"}})
	got, err := NewTeamPrompt(mission, objective, directory, EventNotes{})
	if err != nil || !strings.Contains(got.String(), "Watch the services.\n\nYour teammates (team ops)") || !strings.HasSuffix(got.String(), "- worker-1: Deployer. Runs on any node.\n\nThe task:\nWarn worker-1.") {
		t.Errorf("prompt = %q %v", got, err)
	}
	big, _ := NewMission(strings.Repeat("m", 2000))
	huge, _ := NewObjective(strings.Repeat("o", 5900))
	got, err = NewTeamPrompt(big, huge, directory, EventNotes{})
	if err != nil || strings.Contains(got.String(), "teammates") || !strings.HasSuffix(got.String(), "o") {
		t.Errorf("full prompt keeps mission and objective, drops the directory: %v", err)
	}
}
