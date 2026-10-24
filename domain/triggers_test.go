package domain

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

const testSecret = "s3cret-s3cret-s3cret"

func TestTriggerDefinitions(t *testing.T) {
	good := []TriggerSnapshot{
		{Type: "manual"},
		{Type: "webhook", Name: "deploy", Objective: "Handle {{.type}} for {{.service}}", Secret: testSecret},
		{Type: "webhook", Name: "alerts", Objective: "Look at {{.alert}}", SigningKey: testSecret},
		{Type: "timer", Name: "tick", Objective: "say the time", Every: "10m"},
		{Type: "timer", Name: "nightly", Objective: "report", Cron: "0 3 * * 1-5"},
	}
	triggers, err := NewTriggers(good)
	if err != nil {
		t.Fatal(err)
	}
	if !triggers.Has(TriggerManual) || !triggers.Has(TriggerWebhook) || !triggers.Has(TriggerTimer) || len(triggers.Timers()) != 2 {
		t.Errorf("triggers = %+v", triggers.Snapshot())
	}
	deploy, ok := triggers.Named("deploy")
	if !ok || deploy.Ref() != (TriggerRef{Type: TriggerWebhook, Name: "deploy"}) || deploy.ObjectiveTemplate() != "Handle {{.type}} for {{.service}}" {
		t.Errorf("deploy = %+v", deploy.Snapshot())
	}
	if _, ok := triggers.Named("missing"); ok {
		t.Error("missing trigger found")
	}
	tick, _ := triggers.Named("tick")
	if every, cron := tick.Schedule().Snapshot(); every != "10m0s" || cron != "" || tick.Schedule().Key() != "every 10m0s" {
		t.Errorf("tick schedule = %q %q", every, cron)
	}
	nightly, _ := triggers.Named("nightly")
	if every, cron := nightly.Schedule().Snapshot(); every != "" || cron != "0 3 * * 1-5" {
		t.Errorf("nightly schedule = %q %q", every, cron)
	}
	snap := triggers.Snapshot()
	if snap[1].Secret != testSecret || snap[1].Name != "deploy" || snap[0].Name != "manual" {
		t.Errorf("snapshot = %+v", snap)
	}
	redacted := snap[1].Redacted()
	if redacted.Secret != Redacted || snap[2].Redacted().SigningKey != Redacted || snap[0].Redacted().Secret != "" {
		t.Errorf("redacted = %+v", redacted)
	}
	back, err := NewTriggers(snap)
	if err != nil || len(back.List()) != 5 {
		t.Fatalf("round trip: %v", err)
	}

	bad := map[string]TriggerSnapshot{
		"manual with objective":      {Type: "manual", Objective: "x"},
		"webhook without credential": {Type: "webhook", Objective: "x"},
		"webhook with schedule":      {Type: "webhook", Objective: "x", Secret: testSecret, Every: "1m"},
		"webhook short secret":       {Type: "webhook", Objective: "x", Secret: "short"},
		"webhook secret with space":  {Type: "webhook", Objective: "x", Secret: "a secret with spaces in"},
		"webhook without objective":  {Type: "webhook", Secret: testSecret},
		"webhook bad template":       {Type: "webhook", Objective: "{{.type", Secret: testSecret},
		"timer with secret":          {Type: "timer", Objective: "x", Every: "1m", Secret: testSecret},
		"timer without schedule":     {Type: "timer", Objective: "x"},
		"timer with both":            {Type: "timer", Objective: "x", Every: "1m", Cron: "* * * * *"},
		"timer too fast":             {Type: "timer", Objective: "x", Every: "10ms"},
		"timer bad duration":         {Type: "timer", Objective: "x", Every: "soon"},
		"timer bad cron":             {Type: "timer", Objective: "x", Cron: "* * *"},
		"bad name":                   {Type: "manual", Name: "Bad Name"},
		"unknown type":               {Type: "unit"},
	}
	for name, definition := range bad {
		if _, err := NewTrigger(definition); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
	if _, err := NewTriggers([]TriggerSnapshot{{Type: "webhook", Name: "a", Objective: "x", Secret: testSecret}, {Type: "timer", Name: "a", Objective: "x", Every: "1m"}}); err == nil {
		t.Error("repeated name accepted")
	}
	if _, err := NewTriggers([]TriggerSnapshot{{Type: "webhook", Objective: "x", Secret: testSecret}, {Type: "webhook", Name: "second", Objective: "x", Secret: testSecret}}); err != nil {
		t.Error("two webhooks with distinct names refused:", err)
	}
}

func TestWebhookAuthentication(t *testing.T) {
	body := []byte(`{"type":"deploy"}`)
	bySecret, _ := NewTrigger(TriggerSnapshot{Type: "webhook", Objective: "x", Secret: testSecret})
	if !bySecret.Authenticates(testSecret, "", body) || bySecret.Authenticates("wrong-wrong-wrong-wrong", "", body) || bySecret.Authenticates("", "", body) {
		t.Error("secret check wrong")
	}
	mac := hmac.New(sha256.New, []byte(testSecret))
	mac.Write(body)
	signature := hex.EncodeToString(mac.Sum(nil))
	byKey, _ := NewTrigger(TriggerSnapshot{Type: "webhook", Objective: "x", SigningKey: testSecret})
	if !byKey.Authenticates("", "sha256="+signature, body) || !byKey.Authenticates("", strings.ToUpper(signature), body) {
		t.Error("valid signature refused")
	}
	if byKey.Authenticates("", "sha256="+signature, []byte(`{"type":"other"}`)) || byKey.Authenticates(testSecret, "", body) || byKey.Authenticates("", "sha256=00", body) {
		t.Error("bad signature accepted")
	}
	timer, _ := NewTrigger(TriggerSnapshot{Type: "timer", Objective: "x", Every: "1m"})
	if timer.Authenticates(testSecret, signature, body) {
		t.Error("a timer authenticated a request")
	}
}

func TestSchedules(t *testing.T) {
	base := time.Date(2026, 10, 24, 10, 7, 30, 0, time.UTC)
	every, err := NewSchedule("10m", "")
	if err != nil || !every.ArmsAtOnce() || !every.Next(base).Equal(base.Add(10*time.Minute)) || every.IsZero() {
		t.Errorf("every: %v %v", err, every.Next(base))
	}
	if !(Schedule{}).IsZero() {
		t.Error("zero schedule not zero")
	}
	if e, c := (Schedule{}).Snapshot(); e != "" || c != "" {
		t.Error("zero snapshot")
	}
	cases := []struct {
		cron string
		want time.Time
	}{
		{"* * * * *", time.Date(2026, 10, 24, 10, 8, 0, 0, time.UTC)},
		{"*/10 * * * *", time.Date(2026, 10, 24, 10, 10, 0, 0, time.UTC)},
		{"0 3 * * *", time.Date(2026, 10, 25, 3, 0, 0, 0, time.UTC)},
		{"30 10 * * 6", time.Date(2026, 10, 24, 10, 30, 0, 0, time.UTC)}, // 24 Oct 2026 is a Saturday
		{"0 0 1 1 *", time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)},
		{"0 12 25 * 1", time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)}, // day 25 or a Monday: the 25th comes first
		{"0 12 26 * 0", time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)}, // Sunday the 25th before the 26th
		{"5,35 8-9 * * 1-5", time.Date(2026, 10, 26, 8, 5, 0, 0, time.UTC)},
		{"0 0 * * 7", time.Date(2026, 10, 25, 0, 0, 0, 0, time.UTC)},
		{"15/20 * * * *", time.Date(2026, 10, 24, 10, 15, 0, 0, time.UTC)},
		{"0 0 29 2 *", time.Date(2028, 2, 29, 0, 0, 0, 0, time.UTC)},
	}
	for _, c := range cases {
		s, err := NewSchedule("", c.cron)
		if err != nil {
			t.Errorf("%q: %v", c.cron, err)
			continue
		}
		if s.ArmsAtOnce() || s.Key() != "cron "+c.cron {
			t.Errorf("%q: arms at once or key %q", c.cron, s.Key())
		}
		if got := s.Next(base); !got.Equal(c.want) {
			t.Errorf("%q next after %s = %s, want %s", c.cron, base, got, c.want)
		}
	}
	for _, bad := range []string{"", "* * * *", "60 * * * *", "* 24 * * *", "* * 0 * *", "* * * 13 *", "* * * * 8", "a * * * *", "*/0 * * * *", "5-1 * * * *", "1-70 * * * *"} {
		if _, err := NewSchedule("", bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	for _, bad := range [][2]string{{"", ""}, {"1m", "* * * * *"}, {"1ms", ""}, {"soon", ""}, {"-1m", ""}} {
		if _, err := NewSchedule(bad[0], bad[1]); err == nil {
			t.Errorf("%v accepted", bad)
		}
	}
	never, _ := NewSchedule("", "0 0 31 2 *")
	if !never.Next(base).IsZero() {
		t.Error("31 February found")
	}
	madrid, _ := time.LoadLocation("Europe/Madrid")
	local, _ := NewSchedule("", "0 9 * * *")
	if got := local.Next(base.In(madrid)); got.Hour() != 9 || got.Location() != madrid {
		t.Errorf("cron ignores the location: %s", got)
	}
}

func TestObjectiveTemplates(t *testing.T) {
	event := map[string]any{"type": "deploy", "service": "web", "nested": map[string]any{"n": 1}}
	objective, err := RenderObjective("Handle {{.type}} for {{.service}} ({{.nested.n}})", event)
	if err != nil || objective != "Handle deploy for web (1)" {
		t.Errorf("rendered %q, %v", objective, err)
	}
	if _, err := RenderObjective("Handle {{.missing}}", event); err == nil {
		t.Error("missing field rendered")
	}
	if _, err := RenderObjective("{{.type", event); err == nil {
		t.Error("bad template rendered")
	}
	if _, err := RenderObjective("{{.none}}", map[string]any{"none": ""}); err == nil {
		t.Error("empty objective accepted")
	}
	if err := ValidateObjectiveTemplate(""); err == nil {
		t.Error("empty template accepted")
	}
}

func TestTimer(t *testing.T) {
	timer, err := NewTimer("bot", "tick", "every 10m0s", t0)
	if err != nil || timer.Key() != "bot~tick" || TimerKey("bot", "tick") != timer.Key() || timer.BotID() != "bot" || timer.Trigger() != "tick" || timer.ScheduleKey() != "every 10m0s" {
		t.Fatalf("new timer: %v %+v", err, timer.Snapshot())
	}
	if !timer.Due(t0) || timer.Due(t0.Add(-time.Second)) || !timer.Due(t0.Add(time.Hour)) {
		t.Error("due wrong")
	}
	if err := timer.Fired("task-1", t0.Add(time.Second), t0); err == nil {
		t.Error("next not after the fire accepted")
	}
	if err := timer.Fired("task-1", t0.Add(time.Second), t0.Add(10*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if timer.LastTaskID() != "task-1" || !timer.LastScheduledAt().Equal(t0) || !timer.LastFiredAt().Equal(t0.Add(time.Second)) || !timer.NextFireAt().Equal(t0.Add(10*time.Minute)) {
		t.Errorf("after fire: %+v", timer.Snapshot())
	}
	if err := timer.Rearm("cron 0 * * * *", t0.Add(time.Hour)); err != nil || timer.ScheduleKey() != "cron 0 * * * *" {
		t.Error("rearm failed", err)
	}
	if err := timer.Rearm("", t0); err == nil {
		t.Error("rearm without schedule accepted")
	}
	raw, _ := json.Marshal(timer.Snapshot())
	var snap TimerSnapshot
	_ = json.Unmarshal(raw, &snap)
	back, err := RestoreTimer(snap)
	again, _ := json.Marshal(back.Snapshot())
	if err != nil || string(again) != string(raw) {
		t.Errorf("round trip: %v\n%s\n%s", err, again, raw)
	}
	for name, bad := range map[string]TimerSnapshot{
		"no bot":     {Trigger: "tick", Schedule: "x", NextFireAt: t0},
		"no trigger": {BotID: "bot", Schedule: "x", NextFireAt: t0},
		"no next":    {BotID: "bot", Trigger: "tick", Schedule: "x"},
		"bad task":   {BotID: "bot", Trigger: "tick", Schedule: "x", NextFireAt: t0, LastTaskID: "Bad"},
	} {
		if _, err := RestoreTimer(bad); err == nil {
			t.Errorf("%s restored", name)
		}
	}
	if _, err := NewTimer("bot", "tick", "", t0); err == nil {
		t.Error("timer without schedule accepted")
	}
}

func TestFireKeys(t *testing.T) {
	long := strings.Repeat("a", 64)
	byDelivery := FireKeyWebhook(long, "delivery-1", []byte(`{"a":1}`))
	if _, err := NewIdempotencyKey(byDelivery.String()); err != nil {
		t.Error("webhook key too long:", err)
	}
	if byDelivery != FireKeyWebhook(long, "delivery-1", []byte(`{"a":2}`)) {
		t.Error("the delivery id should decide the key, not the body")
	}
	if FireKeyWebhook("w", "", []byte(`{"a":1}`)) == FireKeyWebhook("w", "", []byte(`{"a":2}`)) || FireKeyWebhook("w", "", []byte(`{"a":1}`)) != FireKeyWebhook("w", "", []byte(`{"a":1}`)) {
		t.Error("body hash wrong")
	}
	timerKey := FireKeyTimer(long, t0)
	if _, err := NewIdempotencyKey(timerKey.String()); err != nil || !strings.HasPrefix(timerKey.String(), "timer:"+long+":") {
		t.Errorf("timer key %q: %v", timerKey, err)
	}
}

func TestTaskAttribution(t *testing.T) {
	task, _ := NewTask("task-1", "bot", "o", "k", "trace-1", t0)
	if task.Trigger() != ManualTrigger() || task.Input() != nil {
		t.Error("a new task is manual without input")
	}
	if err := task.Attribute(TriggerRef{Type: TriggerWebhook, Name: "deploy"}, []byte(`{"type":"deploy"}`)); err != nil {
		t.Fatal(err)
	}
	if err := task.Attribute(TriggerRef{Type: TriggerWebhook, Name: "deploy"}, []byte(`not json`)); err == nil {
		t.Error("bad input accepted")
	}
	if err := task.Attribute(TriggerRef{Type: TriggerWebhook, Name: "deploy"}, make([]byte, MaxTaskInputBytes+1)); err == nil {
		t.Error("large input accepted")
	}
	if err := task.Attribute(TriggerRef{Type: "odd", Name: "deploy"}, nil); err == nil {
		t.Error("bad trigger accepted")
	}
	raw, _ := json.Marshal(task.Snapshot())
	var snap TaskSnapshot
	_ = json.Unmarshal(raw, &snap)
	back, err := RestoreTask(snap)
	if err != nil || back.Trigger().Name != "deploy" || string(back.Input()) != `{"type":"deploy"}` {
		t.Errorf("round trip: %v %+v", err, back.Snapshot())
	}
	snap.Trigger = TriggerRefSnapshot{}
	snap.Input = nil
	if old, err := RestoreTask(snap); err != nil || old.Trigger() != ManualTrigger() {
		t.Error("an old task without a trigger should be manual:", err)
	}
	if ref, err := NewTriggerRef("timer", ""); err != nil || ref.Name != "timer" {
		t.Error("name should default to the type")
	}
	if _, err := NewTriggerRef("", "named"); err == nil {
		t.Error("a name without a type accepted")
	}
}

func TestExecutionCorrelationAndCancel(t *testing.T) {
	e := sampleExecution(t)
	if err := e.RecordCorrelation(true, "/x"); err == nil {
		t.Error("recorded before correlated")
	}
	if err := e.Correlate(""); err == nil {
		t.Error("empty trace accepted")
	}
	if err := e.Correlate("trace-1"); err != nil {
		t.Fatal(err)
	}
	if c := e.Correlation(); c.TraceID != "trace-1" || c.BotID != "bot-1" || c.TaskID != "task-1" || c.ExecutionID != "exec-1" || c.Carried || e.TraceID() != "trace-1" {
		t.Errorf("correlation = %+v", c)
	}
	if err := e.RecordCorrelation(true, "/correlation/exec-1.json"); err != nil || !e.Correlation().Carried {
		t.Error("record failed", err)
	}
	if err := e.Cancel("", t0); err == nil {
		t.Error("cancel without reason accepted")
	}
	if err := e.Cancel("cancelled by cli:op", t0.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if e.Outcome() != ExecutionCancelled || e.FailureReason() != "cancelled by cli:op" || !e.Outcome().IsFinal() {
		t.Errorf("cancelled = %+v", e.Snapshot())
	}
	raw, _ := json.Marshal(e.Snapshot())
	var snap ExecutionSnapshot
	_ = json.Unmarshal(raw, &snap)
	back, err := RestoreExecution(snap)
	if err != nil || back.Outcome() != ExecutionCancelled || back.Correlation() != e.Correlation() {
		t.Errorf("round trip: %v %+v", err, back.Snapshot())
	}
	snap.Correlation = &CorrelationRecord{}
	if _, err := RestoreExecution(snap); err == nil {
		t.Error("empty correlation restored")
	}
	if _, err := ParseExecutionOutcome("Cancelled"); err != nil {
		t.Error(err)
	}
}

func TestBotRedefineAndRedaction(t *testing.T) {
	bot := sampleBot(t)
	tools, _ := NewAllowedTools([]string{"local_read", "local_exec"})
	rules, _ := RestoreToolRules(ToolRulesSnapshot{Allow: []ToolRuleSnapshot{{Tool: "local_exec", Arguments: map[string]string{"program": "ls"}}}}, tools)
	if err := bot.Redefine(bot.Mission(), tools, bot.ModelPolicy(), bot.Budgets()); err != nil {
		t.Fatal(err)
	}
	if err := bot.Govern(AutonomyObserve, rules); err != nil {
		t.Fatal(err)
	}
	narrower, _ := NewAllowedTools([]string{"local_read"})
	if err := bot.Redefine(bot.Mission(), narrower, bot.ModelPolicy(), bot.Budgets()); err == nil {
		t.Error("a list that strands a rule accepted")
	}
	mission, _ := NewMission("A new mission")
	if err := bot.Redefine(mission, tools, bot.ModelPolicy(), bot.Budgets()); err != nil || bot.Mission() != mission {
		t.Error("redefine failed", err)
	}
	bot.Touch(t0.Add(time.Hour))
	if !bot.UpdatedAt().Equal(t0.Add(time.Hour)) {
		t.Error("touch failed")
	}
	triggers, _ := NewTriggers([]TriggerSnapshot{{Type: "webhook", Name: "w", Objective: "x", Secret: testSecret}})
	bot.Wire(triggers, Placement{})
	if trigger, ok := bot.Trigger("w"); !ok || trigger.Type() != TriggerWebhook {
		t.Error("trigger lookup failed")
	}
	if bot.Snapshot().Redacted().Triggers[0].Secret != Redacted || bot.Snapshot().Triggers[0].Secret != testSecret {
		t.Error("redaction wrong")
	}
	if _, err := RestoreBot(bot.Snapshot().Redacted()); err == nil {
		t.Error("a redacted snapshot restored as if the marker were the secret")
	}
	for _, kind := range []EventType{EventTriggerFired, EventTaskCancelled, EventBotUpdated} {
		if _, err := ParseEventType(kind.String()); err != nil || kind.AboutNode() {
			t.Error(kind, err)
		}
	}
	event, _ := NewEvent("evt-1", EventTaskStarted, "t", "bot", "task-1", "trace-1", t0, nil)
	event.ExecutionID = "exec-1"
	if raw, _ := json.Marshal(event); !strings.Contains(string(raw), `"executionId":"exec-1"`) {
		t.Error("execution id not in the event")
	}
}
