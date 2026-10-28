package domain

import (
	"strings"
	"testing"
	"time"
)

func TestTeams(t *testing.T) {
	teams, err := NewTeams([]string{"port", "air"})
	if err != nil {
		t.Fatal(err)
	}
	if got := teams.Snapshot(); len(got) != 2 || got[0] != "air" || got[1] != "port" {
		t.Errorf("snapshot = %v", got)
	}
	if !teams.Has("port") || teams.Has("sea") || teams.Empty() {
		t.Errorf("teams = %+v", teams)
	}
	other, _ := NewTeams([]string{"port", "sea"})
	if shared := teams.Shared(other); len(shared) != 1 || shared[0] != "port" {
		t.Errorf("shared = %v", shared)
	}
	if none, _ := NewTeams(nil); !none.Empty() || none.Snapshot() != nil {
		t.Errorf("no teams = %+v", none)
	}
	for _, bad := range [][]string{{"Port"}, {"a", "a"}, {"a", "b", "c", "d", "e", "f", "g", "h", "i"}, {""}} {
		if _, err := NewTeams(bad); err == nil {
			t.Errorf("%v accepted", bad)
		}
	}
	names := teams.Names()
	names[0] = "changed"
	if teams.Names()[0] != "air" {
		t.Error("Names shares its slice")
	}
}

func TestMissionPurpose(t *testing.T) {
	cases := map[string]string{
		"Fly the coast at dawn. Then land.":      "Fly the coast at dawn.",
		"Watch the quay!  Report.":               "Watch the quay!",
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
	nodes, _ := NewPlacement([]string{"quad-1", "quad-2"}, nil)
	labels, _ := NewPlacement(nil, map[string]string{"zone": "eu", "gpu": "yes"})
	cases := map[string]Placement{"node quad-1, quad-2": nodes, "nodes labelled gpu=yes, zone=eu": labels, "any node": {}}
	for want, p := range cases {
		if got := p.Where(); got != want {
			t.Errorf("where = %q, want %q", got, want)
		}
	}
}

func teamTestBot(t *testing.T, id, tenant string, teams []string) Bot {
	t.Helper()
	bot, err := NewBot(BotID(id), TenantID(tenant), BotName(id), "operator", Mission("You are "+id+". You fly."), AllowedTools{}, ModelPolicy{}, Budgets{}, time.Unix(1, 0))
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
	plane := teamTestBot(t, "plane", "default", []string{"port"})
	quad := teamTestBot(t, "quad-1", "default", []string{"port", "sea"})
	stranger := teamTestBot(t, "stranger", "default", []string{"air"})
	elsewhere := teamTestBot(t, "elsewhere", "harbour", []string{"port"})
	if !plane.IsTeammateOf(quad) || plane.IsTeammateOf(plane) || plane.IsTeammateOf(stranger) || plane.IsTeammateOf(elsewhere) {
		t.Error("teammates are wrong")
	}
	if !quad.TakesDelegationFrom(plane) || stranger.TakesDelegationFrom(plane) {
		t.Error("consent through the team is wrong")
	}
	refuse := false
	delegation, err := NewDelegation(DelegationSnapshot{FromTeam: &refuse})
	if err != nil {
		t.Fatal(err)
	}
	if err := quad.SetDelegation(delegation); err != nil {
		t.Fatal(err)
	}
	if quad.TakesDelegationFrom(plane) {
		t.Error("fromTeam false still takes the team's tasks")
	}
	triggers, _ := NewTriggers([]TriggerSnapshot{{Type: "manual"}, {Type: "delegation"}})
	quad.Wire(triggers, Placement{})
	if !quad.TakesDelegationFrom(plane) || !quad.TakesDelegationFrom(stranger) {
		t.Error("the delegation trigger is consent for anyone")
	}
	if s := quad.Delegation().Snapshot(); s == nil || s.FromTeam == nil || *s.FromTeam {
		t.Errorf("snapshot = %+v", s)
	}
	if s := (Delegation{}).Snapshot(); s != nil {
		t.Errorf("empty snapshot = %+v", s)
	}
	mate := TeammateOf(plane, quad)
	if mate.ID != "quad-1" || len(mate.Teams) != 1 || mate.Purpose != "You are quad-1." || mate.Where != "any node" || !mate.TakesDelegation {
		t.Errorf("teammate = %+v", mate)
	}
}

func TestBotSnapshotKeepsTheTeams(t *testing.T) {
	bot := teamTestBot(t, "plane", "default", []string{"port"})
	bot.Register(time.Unix(2, 0))
	s := bot.Snapshot()
	if len(s.Groups) != 1 || s.Groups[0] != "port" {
		t.Fatalf("groups = %v", s.Groups)
	}
	s.Model = ModelPolicySnapshot{Model: "m", RoutingPolicy: "cost-optimized"}
	s.Budgets = BudgetsSnapshot{MaxModelCallsPerTask: 1, MaxToolCallsPerTask: 1, TaskTimeoutSeconds: 60}
	s.Level = "observe"
	restored, err := RestoreBot(s)
	if err != nil || !restored.Teams().Has("port") {
		t.Fatalf("restored = %+v %v", restored.Snapshot().Groups, err)
	}
	s.Groups = []string{"Bad"}
	if _, err := RestoreBot(s); err == nil {
		t.Error("a bad team was restored")
	}
}

func TestTeamDirectoryRenders(t *testing.T) {
	mates := []Teammate{
		{ID: "quad-1", Teams: []TeamName{"port"}, Purpose: "Pilot of quad-1.", Tools: []string{"run_mission"}, Where: "node quad-1", TakesDelegation: true},
		{ID: "quad-2", Teams: []TeamName{"port", "sea"}, Purpose: "Pilot of quad-2.", Where: "any node"},
	}
	one := NewTeamDirectory([]TeamName{"port"}, mates)
	text := one.Render(4000)
	want := "Your teammates (team port), bots of your operator that work with you:\n- quad-1: Pilot of quad-1. Tools: run_mission. Runs on node quad-1. Takes delegated tasks.\n- quad-2: Pilot of quad-2. Runs on any node."
	if text != want {
		t.Errorf("render =\n%s\nwant\n%s", text, want)
	}
	many := NewTeamDirectory([]TeamName{"port", "sea"}, mates)
	if text := many.Render(4000); !strings.Contains(text, "(teams port, sea)") || !strings.Contains(text, "- quad-2 (port, sea): ") {
		t.Errorf("many teams =\n%s", text)
	}
	heading := len("Your teammates (team port), bots of your operator that work with you:\n")
	cut := one.Render(heading + len("… and 2 more\n"))
	if cut != "Your teammates (team port), bots of your operator that work with you:\n… and 2 more" {
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
	if text := NewTeamDirectory([]TeamName{"port"}, lots).Render(8000); strings.Count(text, "\n- ") != MaxDirectoryMembers || !strings.HasSuffix(text, "… and 8 more") {
		t.Errorf("capped =\n%s", text)
	}
}

func TestTeamPromptFitsTheLimit(t *testing.T) {
	mission, _ := NewMission("Fly the plane.")
	objective, _ := NewObjective("Warn quad-1.")
	plain, _ := NewPrompt(mission, objective)
	if got, err := NewTeamPrompt(mission, objective, TeamDirectory{}, EventNotes{}); err != nil || got != plain {
		t.Errorf("no directory = %q %v", got, err)
	}
	directory := NewTeamDirectory([]TeamName{"port"}, []Teammate{{ID: "quad-1", Purpose: "Pilot.", Where: "any node"}})
	got, err := NewTeamPrompt(mission, objective, directory, EventNotes{})
	if err != nil || !strings.Contains(got.String(), "Fly the plane.\n\nYour teammates (team port)") || !strings.HasSuffix(got.String(), "- quad-1: Pilot. Runs on any node.\n\nThe task:\nWarn quad-1.") {
		t.Errorf("prompt = %q %v", got, err)
	}
	big, _ := NewMission(strings.Repeat("m", 2000))
	huge, _ := NewObjective(strings.Repeat("o", 5900))
	got, err = NewTeamPrompt(big, huge, directory, EventNotes{})
	if err != nil || strings.Contains(got.String(), "teammates") || !strings.HasSuffix(got.String(), "o") {
		t.Errorf("full prompt keeps mission and objective, drops the directory: %v", err)
	}
}
