package activityaudit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/underpass-ai/axlr-bots/domain"
)

// arreglo is a dev program that failed the same way twice in a task, the
// ids, shas and paths of the two runs different.
func arregloTask(source *fakeSource, id, bot string, minute int) {
	source.add(id, bot, "Completed", "", at(minute),
		started(id+"-c1", toolLocalExec, localExec("/home/gx10a/Documents/ai/bots-work-teams/teams/dev/programs/bin/arreglo", "plan", "--task", "task-0dc89689b19a575f87602c94")),
		ended(id+"-c1", toolLocalExec, "failed", execFailed("call_1", 2, "arreglo: no plan for task-0dc89689b19a575f87602c94 at /home/gx10a/x (head 1a2b3c4d)"), minute),
		started(id+"-c2", toolLocalExec, localExec("/home/tirso/bin/arreglo", "plan", "--task", "task-5dbfaeddfdb4f706d3a45963")),
		ended(id+"-c2", toolLocalExec, "failed", execFailed("call_2", 2, "arreglo: no plan for task-5dbfaeddfdb4f706d3a45963 at /home/tirso/y (head 9f8e7d6c)"), minute+1),
	)
}

func audit(t *testing.T, auditor *Auditor, ids ...string) {
	t.Helper()
	for _, id := range ids {
		if err := auditor.Audit(context.Background(), id); err != nil {
			t.Fatal(err)
		}
	}
}

// Two failed local_exec of one program and exit code in a task are one
// signature seen twice and one event; the same failure in a later task
// publishes nothing and grows the count; a restart keeps it all.
func TestOneSignatureOneEventEver(t *testing.T) {
	dir := t.TempDir()
	source := newFakeSource(team("dev", "dev-jefe", "dev-go"), team("qa", "qa-jefe"))
	publisher := &fakePublisher{}
	arregloTask(source, "task-a", "dev-jefe", 1)
	auditor, store := auditorFor(t, source, publisher, dir)
	audit(t, auditor, "task-a")
	published := publisher.all()
	if len(published) != 1 {
		t.Fatalf("published %d: %+v", len(published), published)
	}
	signatures := store.Signatures()
	if len(signatures) != 1 || signatures[0].Count != 2 || signatures[0].Program != "arreglo" {
		t.Fatalf("signatures = %+v", signatures)
	}
	command := published[0]
	id := signatures[0].ID
	if command.Group != "qa" || command.Name != EventName || command.IdempotencyKey != "activity:"+id {
		t.Errorf("command = %+v", command)
	}
	want := map[string]any{"id": id, "kind": KindToolCall, "bot": "dev-jefe", "team": "dev", "task": "task-a", "tool": "local_exec", "program": "arreglo", "error": "arreglo: no plan for <id> at <path> (head <hex>)", "count": json.Number("2"), "first_at": stamp(at(1))}
	if raw, _ := json.Marshal(command.Data); string(raw) != mustJSON(want) {
		t.Errorf("data = %s\nwant %s", raw, mustJSON(want))
	}
	if want := id + " · local_exec · dev-jefe (dev) · arreglo · arreglo: no plan for <id> at <path> (head <hex>)"; command.Message != want {
		t.Errorf("message = %q, want %q", command.Message, want)
	}
	if !regexp.MustCompile(`^[A-Za-z0-9._:-]{1,128}$`).MatchString(id) {
		t.Errorf("id %q is not the channel's", id)
	}
	if len([]rune(command.Message)) > domain.MaxEventMessageLength {
		t.Errorf("message = %q", command.Message)
	}

	arregloTask(source, "task-b", "dev-go", 10)
	audit(t, auditor, "task-b", "task-a", "task-b")
	if len(publisher.all()) != 1 {
		t.Fatalf("a seen signature published again: %+v", publisher.all())
	}
	if got := store.Signatures()[0]; got.Count != 4 || strings.Join(got.Bots, ",") != "dev-go,dev-jefe" || got.LastTask != "task-b" || got.FirstTask != "task-a" {
		t.Errorf("after the second task: %+v", got)
	}

	// A restart reads what the first run kept.
	arregloTask(source, "task-c", "dev-go", 20)
	restarted, reopened := auditorFor(t, source, publisher, dir)
	audit(t, restarted, "task-b", "task-c")
	if len(publisher.all()) != 1 {
		t.Fatalf("published after a restart: %+v", publisher.all())
	}
	if got := reopened.Signatures()[0]; got.Count != 6 || got.EventID != "evt-published-1" || got.PublishedAt == "" {
		t.Errorf("after the restart: %+v", got)
	}
	info, err := os.Stat(filepath.Join(dir, SeenFileName))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("seen.json: %v %v", info, err)
	}
}

func mustJSON(v any) string {
	raw, _ := json.Marshal(v)
	return string(raw)
}

// The failures of today's fleet, in the shapes it writes them, make the
// signatures they should, and the noise rules drop what they should.
func TestTheFleetsFailures(t *testing.T) {
	source := newFakeSource(team("dev", "dev-go"), team("qa", "qa-explorador", "qa-jefe"), team("marketing", "mkt-vigia"))
	publisher := &fakePublisher{}
	// A QA probe's refusal, its program shown: QA testing a product.
	source.add("task-probe", "qa-explorador", "Completed", "", at(1),
		started("p1", toolLocalExec, localExec("/x/qa-probe", "cli", "--sandbox", "s1")),
		ended("p1", toolLocalExec, "failed", execFailed("p1", 1, "refused"), 1))
	// A QA bot's local_exec whose program the cut summary hid: its probes.
	source.add("task-qa-cut", "qa-explorador", "Completed", "", at(2),
		started("q1", toolLocalExec, cutSummary),
		ended("q1", toolLocalExec, "failed", cutEnvelope, 2))
	// A marketing watcher's, hidden the same way: the bot stands in.
	source.add("task-mkt", "mkt-vigia", "Completed", "", at(3),
		started("m1", toolLocalExec, cutSummary),
		ended("m1", toolLocalExec, "failed", cutEnvelope, 3),
		started("m2", toolLocalExec, cutSummary),
		ended("m2", toolLocalExec, "failed", cutEnvelope, 3),
		bridged("m3", "kmp/kmp_write_memory"),
		ended("m3", toolCallTool, "failed", bridgedCut, 4))
	// The model endpoint's 504 ended a task.
	source.add("task-504", "dev-go", "Failed", provider504, at(5))
	auditor, store := auditorFor(t, source, publisher, "")
	audit(t, auditor, "task-probe", "task-qa-cut", "task-mkt", "task-504")
	got := map[string]SeenSignature{}
	for _, s := range store.Signatures() {
		got[What(s)+" | "+s.Error] = s
	}
	want := map[string]int{
		"local_exec (partial) | " + cutMarker:                                          2,
		"axlr_call_tool kmp/kmp_write_memory | " + cutMarker:                           1,
		"task failed | interrupted: model endpoint <addr> provider_failure (HTTP 504)": 1,
	}
	if len(got) != len(want) {
		t.Fatalf("signatures = %+v", got)
	}
	for what, count := range want {
		if got[what].Count != count {
			t.Errorf("%s: %+v", what, got[what])
		}
	}
	if s := got["local_exec (partial) | "+cutMarker]; !strings.Contains(s.Key, "bot=mkt-vigia") || !strings.Contains(s.Key, "exit=nonzero") || !s.Partial {
		t.Errorf("partial = %+v", s)
	}
	if len(publisher.all()) != 3 {
		t.Errorf("published %d", len(publisher.all()))
	}
	for _, command := range publisher.all() {
		if command.Data["kind"] == KindTaskFailed && (command.Data["tool"] != nil || command.Data["error"] != "interrupted: model endpoint <addr> provider_failure (HTTP 504)") {
			t.Errorf("task failed = %+v", command.Data)
		}
		if command.Data["mcp_tool"] == "kmp/kmp_write_memory" && command.Data["partial"] != nil {
			t.Errorf("a bridged call that names its tool is not partial: %+v", command.Data)
		}
	}
}

// One model's slip that it fixed at once is noise; the same slip from a
// second bot is not.
func TestARecoveredSlipWaitsForASecondBot(t *testing.T) {
	source := newFakeSource(team("qa", "qa-jefe", "qa-triaje"))
	publisher := &fakePublisher{}
	slip := func(id, bot string, minute int) {
		source.add(id, bot, "Completed", "", at(minute),
			started(id+"-1", "axlr_remember", `{"why":"keep"}`),
			ended(id+"-1", "axlr_remember", "failed", rememberLine, minute),
			started(id+"-2", "axlr_remember", `{"continuation":"read_d3f95e75"}`),
			ended(id+"-2", "axlr_remember", "ok", "", minute))
	}
	slip("task-1", "qa-jefe", 1)
	slip("task-2", "qa-jefe", 2)
	auditor, store := auditorFor(t, source, publisher, "")
	audit(t, auditor, "task-1", "task-2")
	if len(publisher.all()) != 0 {
		t.Fatalf("one bot's recovered slip published: %+v", publisher.all())
	}
	if s := store.Signatures()[0]; s.Count != 2 || s.Recovered != 2 {
		t.Fatalf("%+v", s)
	}
	slip("task-3", "qa-triaje", 3)
	audit(t, auditor, "task-3")
	if len(publisher.all()) != 1 || publisher.all()[0].Data["count"] != json.Number("3") {
		t.Fatalf("the second bot's: %+v", publisher.all())
	}
	// A local program recovers by the same program only; a bridged call
	// by the same plugin's tool.
	source.add("task-4", "qa-jefe", "Completed", "", at(4),
		started("a", toolLocalExec, localExec("/x/arreglo", "plan")),
		ended("a", toolLocalExec, "failed", execFailed("a", 1, "no"), 4),
		started("b", toolLocalExec, localExec("/x/other")),
		ended("b", toolLocalExec, "ok", "", 4),
		bridged("c", "kmp/kmp_ask"),
		ended("c", toolCallTool, "failed", `{"error":"bad about"}`, 4),
		bridged("d", "kmp/kmp_ask"),
		ended("d", toolCallTool, "ok", "", 4))
	audit(t, auditor, "task-4")
	for _, s := range store.Signatures() {
		switch {
		case s.Program == "arreglo" && s.Recovered != 0:
			t.Errorf("recovered by another program: %+v", s)
		case s.McpTool == "kmp/kmp_ask" && s.Recovered != 1:
			t.Errorf("not recovered by the same tool: %+v", s)
		}
	}
}

// An ignored program publishes nothing, and so does a partial local_exec
// of a member of the team published to; without the team rule it is
// audited.
func TestIgnoredProgramsPublishNothing(t *testing.T) {
	source := newFakeSource(team("qa", "qa-explorador"))
	for i, program := range DefaultIgnorePrograms {
		source.add("task-"+program, "qa-explorador", "Completed", "", at(i),
			started("c", toolLocalExec, localExec("/x/"+program, "cli")),
			ended("c", toolLocalExec, "failed", execFailed("c", 1, "refused"), i))
	}
	source.add("task-cut", "qa-explorador", "Completed", "", at(9),
		started("c", toolLocalExec, cutSummary),
		ended("c", toolLocalExec, "failed", cutEnvelope, 9))
	publisher := &fakePublisher{}
	auditor, store := auditorFor(t, source, publisher, "")
	audit(t, auditor, "task-qa-probe", "task-qa-term", "task-qa-gui", "task-qa-script", "task-cut")
	if len(publisher.all()) != 0 || len(store.Signatures()) != 0 {
		t.Fatalf("published %+v, kept %+v", publisher.all(), store.Signatures())
	}
	auditor.config.IgnoreTeamPartial = false
	auditor.config.IgnorePrograms = nil
	auditor.config.Store = mustStore(t)
	audit(t, auditor, "task-qa-probe", "task-cut")
	if len(publisher.all()) != 2 {
		t.Fatalf("without the rules: %+v", publisher.all())
	}
}

func mustStore(t *testing.T) *SeenStore {
	t.Helper()
	store, err := OpenSeenStore("")
	if err != nil {
		t.Fatal(err)
	}
	return store
}

// A publish that failed is retried by the next flush, under the same
// key; a dry run publishes nothing and records what it would have.
func TestAFailedPublishIsRetried(t *testing.T) {
	source := newFakeSource(team("dev", "dev-go"))
	source.add("task-504", "dev-go", "Failed", provider504, at(1))
	source.add("task-other", "dev-go", "Failed", "", at(2))
	publisher := &fakePublisher{fail: errors.New("the control plane is down")}
	auditor, store := auditorFor(t, source, publisher, "")
	if err := auditor.Audit(context.Background(), "task-504"); err == nil || !strings.Contains(err.Error(), "the control plane is down") {
		t.Fatalf("err = %v", err)
	}
	if len(store.Pending()) != 1 {
		t.Fatalf("pending = %+v", store.Pending())
	}
	publisher.fail = nil
	audit(t, auditor, "task-other")
	if len(publisher.all()) != 2 || publisher.all()[0].Data["task"] != "task-504" || publisher.all()[1].Data["error"] != "no reason given" {
		t.Fatalf("published %+v", publisher.all())
	}

	dry, dryStore := auditorFor(t, source, nil, "")
	audit(t, dry, "task-504")
	if s := dryStore.Signatures(); len(s) != 1 || s[0].EventID != DryRunEventID {
		t.Fatalf("dry run = %+v", s)
	}
}

// A rearmed signature publishes again on its next occurrence, under a
// new key; one the store does not know is refused.
func TestRearm(t *testing.T) {
	source := newFakeSource(team("dev", "dev-go"))
	source.add("task-1", "dev-go", "Failed", provider504, at(1))
	source.add("task-2", "dev-go", "Failed", provider504, at(2))
	publisher := &fakePublisher{}
	auditor, store := auditorFor(t, source, publisher, t.TempDir())
	audit(t, auditor, "task-1")
	id := store.Signatures()[0].ID
	if _, err := store.Rearm([]string{"fallo-nope"}); err == nil {
		t.Error("an unknown id was rearmed")
	}
	if rearmed, err := store.Rearm([]string{"all"}); err != nil || len(rearmed) != 1 || rearmed[0] != id {
		t.Fatalf("rearm = %v %v", rearmed, err)
	}
	if err := auditor.Flush(context.Background()); err != nil || len(publisher.all()) != 1 {
		t.Fatalf("a rearmed signature published before it was seen again: %v", publisher.all())
	}
	audit(t, auditor, "task-2")
	if all := publisher.all(); len(all) != 2 || all[1].IdempotencyKey != "activity:"+id+":r1" || all[1].Data["count"] != json.Number("1") {
		t.Fatalf("after the rearm: %+v", all)
	}
}

// What is not an end is not audited yet; a task created before the
// cursor is history; an auditor without a source, a store or a valid
// team is refused.
func TestAuditSkips(t *testing.T) {
	source := newFakeSource(team("dev", "dev-go"))
	source.add("task-running", "dev-go", "Running", "", at(30))
	source.add("task-old", "dev-go", "Failed", provider504, at(1))
	publisher := &fakePublisher{}
	auditor, store := auditorFor(t, source, publisher, "")
	store.SetCursor(at(10))
	audit(t, auditor, "task-running", "task-old")
	if len(store.Signatures()) != 0 || store.Audited("task-running") {
		t.Fatalf("audited %+v", store.Signatures())
	}
	if err := auditor.Audit(context.Background(), "task-none"); !errors.As(err, new(domain.NotFoundError)) {
		t.Errorf("missing task: %v", err)
	}
	for _, config := range []AuditorConfig{{Store: store, Team: "qa"}, {Source: source, Team: "qa"}, {Source: source, Store: store, Team: "Not A Team"}} {
		if _, err := NewAuditor(config); err == nil {
			t.Errorf("accepted %+v", config)
		}
	}
	if a, err := NewAuditor(AuditorConfig{Source: source, Store: store, Team: "qa"}); err != nil || a.config.Now == nil || a.config.Log == nil {
		t.Errorf("defaults: %v", err)
	}
}

// Start: a store without a cursor and no since starts now, marking what
// ended audited unread; with since it replays from it; a store with a
// cursor goes on from it and ignores since.
func TestStartAndReconcile(t *testing.T) {
	source := newFakeSource(team("dev", "dev-go"))
	source.add("task-1", "dev-go", "Failed", provider504, at(1))
	source.add("task-2", "dev-go", "Running", "", at(2))
	ctx := context.Background()

	publisher := &fakePublisher{}
	replay, replayStore := auditorFor(t, source, publisher, "")
	if err := replay.Start(ctx, at(0)); err != nil {
		t.Fatal(err)
	}
	if len(publisher.all()) != 1 || !replayStore.Cursor().Equal(at(2)) {
		t.Fatalf("replay: %+v cursor %s", publisher.all(), replayStore.Cursor())
	}

	blank, blankStore := auditorFor(t, source, &fakePublisher{}, "")
	if err := blank.Start(ctx, time.Time{}); err != nil {
		t.Fatal(err)
	}
	if blankStore.Audited("task-2") || !blankStore.Cursor().Equal(at(2)) || len(blankStore.Signatures()) != 0 {
		t.Fatalf("start now: cursor %s %+v", blankStore.Cursor(), blankStore.Signatures())
	}

	// The running task ends: a start from the cursor audits it, and its
	// since is ignored (task-1 is not counted again).
	source.tasks["task-2"].snapshot.Status = "Failed"
	source.tasks["task-2"].snapshot.FailureReason = "stuck"
	if err := blank.Start(ctx, at(0)); err != nil {
		t.Fatal(err)
	}
	if !blankStore.Audited("task-2") || len(blankStore.Signatures()) != 1 || blankStore.Signatures()[0].Error != "stuck" {
		t.Fatalf("reconcile: %+v", blankStore.Signatures())
	}

	// No task at all: the cursor stays.
	empty, emptyStore := auditorFor(t, newFakeSource(), &fakePublisher{}, "")
	if err := empty.Start(ctx, at(5)); err != nil || !emptyStore.Cursor().Equal(at(5)) {
		t.Fatalf("empty: %v %s", err, emptyStore.Cursor())
	}
}

// The DTO's keys are the publish body's data; a task in no team names
// none.
func TestCommandOfABotInNoTeam(t *testing.T) {
	source := newFakeSource()
	source.add("task-1", "loner", "Failed", provider504, at(1))
	publisher := &fakePublisher{}
	auditor, _ := auditorFor(t, source, publisher, "")
	audit(t, auditor, "task-1")
	if data := publisher.all()[0].Data; data["team"] != nil || data["bot"] != "loner" {
		t.Errorf("data = %+v", data)
	}
	if got := auditor.command(&SeenSignature{ID: "fallo-x", Kind: KindTaskFailed, Bots: []string{"b"}, FirstTask: "task-f"}); got.Data["bot"] != "b" || got.Data["task"] != "task-f" {
		t.Errorf("fallback = %+v", got.Data)
	}
}

// The message names the tool, the bot and its team, the program (or the
// plugin's tool, or -) and the error; a failed task's tool is task; a
// long one is cut to 280 characters.
func TestMessage(t *testing.T) {
	for want, s := range map[string]SeenSignature{
		"fallo-1 · task · dev-go (dev) · - · stuck":                       {ID: "fallo-1", Kind: KindTaskFailed, Error: "stuck"},
		"fallo-2 · axlr_call_tool · dev-go (dev) · kmp/kmp_ask · x":       {ID: "fallo-2", Kind: KindToolCall, Tool: toolCallTool, McpTool: "kmp/kmp_ask", Error: "x"},
		"fallo-3 · axlr_remember · loner (-) · - · send the continuation": {ID: "fallo-3", Kind: KindToolCall, Tool: "axlr_remember", Error: "send the continuation"},
	} {
		team := "dev"
		if s.ID == "fallo-3" {
			team = ""
		}
		bot := "dev-go"
		if s.ID == "fallo-3" {
			bot = "loner"
		}
		if got := Message(&s, bot, team); got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	}
	long := SeenSignature{ID: "fallo-4", Kind: KindToolCall, Tool: "t", Error: strings.Repeat("e", 400)}
	if got := []rune(Message(&long, "b", "t")); len(got) != domain.MaxEventMessageLength || got[len(got)-1] != '…' {
		t.Errorf("not cut: %d", len(got))
	}
}

// A backfill publishes at most PerMinute events a minute, oldest first:
// the next waits for the oldest of the minute to leave it.
func TestPacing(t *testing.T) {
	source := newFakeSource(team("dev", "dev-go"))
	for i := range 4 {
		source.add(fmt.Sprintf("task-%d", i), "dev-go", "Failed", fmt.Sprintf("reason %c", 'a'+i), at(i))
	}
	publisher := &fakePublisher{}
	clock := at(0)
	var waited []time.Duration
	store := mustStore(t)
	auditor, err := NewAuditor(AuditorConfig{Source: source, Publisher: publisher, Store: store, Team: "qa", PerMinute: 2,
		Now: func() time.Time { return clock },
		Sleep: func(_ context.Context, d time.Duration) error {
			waited = append(waited, d)
			clock = clock.Add(d)
			return nil
		}})
	if err != nil {
		t.Fatal(err)
	}
	if err := auditor.Start(context.Background(), at(0)); err != nil {
		t.Fatal(err)
	}
	all := publisher.all()
	if len(all) != 4 || len(waited) != 1 || waited[0] != time.Minute {
		t.Fatalf("published %d, waited %v", len(all), waited)
	}
	for i, command := range all {
		if command.Data["error"] != fmt.Sprintf("reason %c", 'a'+i) {
			t.Errorf("not oldest first: %d %v", i, command.Data["error"])
		}
	}
	if _, err := NewAuditor(AuditorConfig{Source: source, Store: store, Team: "qa", PerMinute: -1}); err == nil {
		t.Error("a negative pace was taken")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := sleep(ctx, time.Hour); err == nil {
		t.Error("sleep outlived its context")
	}
	if err := sleep(context.Background(), time.Millisecond); err != nil {
		t.Error(err)
	}
	stuck, _ := NewAuditor(AuditorConfig{Source: source, Publisher: publisher, Store: mustStore(t), Team: "qa", PerMinute: 1, Now: func() time.Time { return at(0) },
		Sleep: func(context.Context, time.Duration) error { return context.Canceled }})
	if err := stuck.Start(context.Background(), at(0)); err == nil {
		t.Error("a pace whose wait ended published anyway")
	}
}
