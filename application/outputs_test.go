package application

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/underpass-ai/axlr-bots/domain"
)

var updateGolden = flag.Bool("update", false, "rewrite the golden files of testdata/outputs")

const outputSecret = "a-webhook-secret-of-32-characters"

// slackURL is a chat webhook's URL: its path is the secret.
const slackURL = "https://hooks.slack.test/services/T000/B000/XXXXXXXXXXXXXXXX"

func addOutput(t *testing.T, h *harness, definition OutputDefinition) OutputView {
	t.Helper()
	view, err := NewAddOutput(h.deps).Execute(context.Background(), definition)
	if err != nil {
		t.Fatal(err)
	}
	return view
}

// golden compares the body with testdata/outputs/<name>.json, indented.
func golden(t *testing.T, name string, body []byte) {
	t.Helper()
	var pretty bytes.Buffer
	if err := json.Indent(&pretty, body, "", "  "); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	pretty.WriteByte('\n')
	path := filepath.Join("testdata", "outputs", name+".json")
	if *updateGolden {
		if err := os.WriteFile(path, pretty.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run go test ./application -run Outputs -update)", err)
	}
	if !bytes.Equal(want, pretty.Bytes()) {
		t.Errorf("%s differs from its golden file:\n%s", name, pretty.String())
	}
}

// deliveriesOf are the target's deliveries, in the order queued.
func deliveriesOf(h *harness, target string) []domain.OutputDelivery {
	var out []domain.OutputDelivery
	for _, d := range h.outbox2.all() {
		if d.Output().String() == target {
			out = append(out, d)
		}
	}
	return out
}

// An incident's life reaches each output that wants its events, as the
// body the target's type takes, with the panel link of the control plane
// or of the target; the filters keep the others out.
func TestOutputsAnnounceAnIncidentsLife(t *testing.T) {
	h := newHarness()
	h.deps.PanelURL = "https://panel.example.test/"
	h.deps.OperatorURL = "https://cp.example.test:7879"
	triage := brigade(t, h, domain.ApprovalsSnapshot{})
	addOutput(t, h, OutputDefinition{Name: "hook", Type: "webhook", URL: "https://receiver.example.test/axlr", Secret: outputSecret})
	addOutput(t, h, OutputDefinition{Name: "chat", Type: "slack", URL: slackURL, PanelURL: "https://ops.example.test/axlr"})
	addOutput(t, h, OutputDefinition{Name: "pager", Type: "command", Command: []string{"/usr/local/bin/page"}, Severities: []string{"sev1"}})
	addOutput(t, h, OutputDefinition{Name: "elsewhere", Type: "webhook", URL: "https://other.example.test/", Secret: outputSecret, Tenants: []string{"another"}})
	addOutput(t, h, OutputDefinition{Name: "ends", Type: "webhook", URL: "https://ends.example.test/", Secret: outputSecret, Events: []string{"resolved", "incident.repeated"}})
	ctx := context.Background()
	incident := proposed(t, h, triage)
	if _, err := NewOpenIncident(h.deps).Execute(ctx, alert(triage, "fp-a")); err != nil {
		t.Fatal(err)
	}
	h.clock.now = t0.Add(12*time.Minute + 30*time.Second)
	if _, err := NewResolveIncident(h.deps).Execute(ctx, incident.ID(), "fixed by hand"); err != nil {
		t.Fatal(err)
	}
	want := []string{"incident.opened", "incident.diagnosed", "incident.proposed", "incident.resolved"}
	for _, target := range []string{"hook", "chat"} {
		got := deliveriesOf(h, target)
		if len(got) != len(want) {
			t.Fatalf("%s: %d deliveries, want %d", target, len(got), len(want))
		}
		for i, d := range got {
			if d.Event().String() != want[i] || d.IncidentID() != incident.ID() || d.Status() != domain.DeliveryPending {
				t.Errorf("%s[%d] = %+v", target, i, d.Snapshot())
			}
			golden(t, target+"-"+strings.TrimPrefix(want[i], "incident."), d.Payload())
		}
	}
	if got := deliveriesOf(h, "ends"); len(got) != 2 || got[0].Event() != domain.EventIncidentRepeated || got[1].Event() != domain.EventIncidentResolved {
		t.Errorf("ends = %d deliveries", len(got))
	}
	if len(deliveriesOf(h, "pager")) != 0 || len(deliveriesOf(h, "elsewhere")) != 0 {
		t.Error("a filter let an event through")
	}
	queued := 0
	for _, a := range h.log.audits {
		if a.Action != "incident.output" {
			continue
		}
		queued++
		if a.Details["status"] != "queued" || a.Details["incident"] != incident.ID().String() || a.Details["target"] == "" || a.Details["event"] == "" {
			t.Errorf("audit = %+v", a)
		}
		for _, value := range a.Details {
			if strings.Contains(value, "XXXX") || strings.Contains(value, outputSecret) {
				t.Errorf("a secret in the audit: %+v", a)
			}
		}
	}
	if queued != 10 {
		t.Errorf("queued audits = %d, want 10", queued)
	}
}

// The same event queued twice is one delivery per target.
func TestOutputQueueingIsIdempotent(t *testing.T) {
	h := newHarness()
	triage := triageBot(t, h, "triage", "")
	addOutput(t, h, OutputDefinition{Name: "hook", Type: "webhook", URL: "https://receiver.example.test/", Secret: outputSecret})
	opened, err := NewOpenIncident(h.deps).Execute(context.Background(), alert(triage, "fp-a"))
	if err != nil {
		t.Fatal(err)
	}
	event, _ := lastOf(h, domain.EventIncidentOpened)
	incidentOutputQueue{deps: h.deps, rec: recorder{deps: h.deps}}.queue(context.Background(), event, opened.Incident)
	if got := len(h.outbox2.deliveries); got != 1 {
		t.Errorf("deliveries = %d, want 1", got)
	}
}

// A queueing that fails is audited, and the incident moves all the same.
func TestAQueueingThatFailsLeavesTheIncidentAlone(t *testing.T) {
	h := newHarness()
	triage := triageBot(t, h, "triage", "")
	addOutput(t, h, OutputDefinition{Name: "hook", Type: "webhook", URL: "https://receiver.example.test/", Secret: outputSecret})
	h.outbox2.fail = errBoom
	opened, err := NewOpenIncident(h.deps).Execute(context.Background(), alert(triage, "fp-a"))
	if err != nil || opened.Incident.Status() != domain.IncidentOpen {
		t.Fatalf("open: %v", err)
	}
	h.outbox2.fail, h.outbox2.getFail = nil, errBoom
	h.clock.now = t0.Add(time.Minute)
	if _, err := NewOpenIncident(h.deps).Execute(context.Background(), alert(triage, "fp-b")); err != nil {
		t.Fatal(err)
	}
	h.outbox2.getFail, h.outs.listFail = nil, errBoom
	if _, err := NewResolveIncident(h.deps).Execute(context.Background(), opened.Incident.ID(), ""); err != nil {
		t.Fatal(err)
	}
	var failed []domain.AuditEntry
	for _, a := range h.log.audits {
		if a.Action == "incident.output" && a.Outcome == domain.AuditFailed {
			failed = append(failed, a)
		}
	}
	if len(failed) != 2 || failed[0].Details["target"] != "hook" || failed[0].Details["reason"] != "boom" || failed[1].Details["target"] != "" || failed[1].Details["event"] != "incident.resolved" {
		t.Errorf("failed audits = %+v", failed)
	}
	if len(h.outbox2.deliveries) != 0 {
		t.Errorf("deliveries = %d", len(h.outbox2.deliveries))
	}
}

// Without the ports (a test's partial wiring) nothing is queued.
func TestOutputQueueWithoutItsPortsDoesNothing(t *testing.T) {
	h := newHarness()
	deps := h.deps
	deps.Outputs = nil
	incidentOutputQueue{deps: deps}.queue(context.Background(), domain.Event{}, domain.Incident{})
	if len(h.log.audits) != 0 {
		t.Error("audited")
	}
}

// A diagnosis past MaxOutputExcerpt is cut and says so.
func TestOutputExcerpt(t *testing.T) {
	if text, cut := outputExcerpt("  "); text != "" || cut {
		t.Errorf("blank = %q %v", text, cut)
	}
	if text, cut := outputExcerpt("short"); text != "short" || cut {
		t.Errorf("short = %q %v", text, cut)
	}
	long := strings.Repeat("é", MaxOutputExcerpt)
	text, cut := outputExcerpt(long)
	if !cut || len(text) > MaxOutputExcerpt || !strings.HasSuffix(text, "…") {
		t.Errorf("long = %d bytes, cut %v", len(text), cut)
	}
}

func queuedDelivery(t *testing.T, h *harness, definition OutputDefinition) domain.OutputDelivery {
	t.Helper()
	triage := triageBot(t, h, "triage", "")
	addOutput(t, h, definition)
	if _, err := NewOpenIncident(h.deps).Execute(context.Background(), alert(triage, "fp-a")); err != nil {
		t.Fatal(err)
	}
	all := h.outbox2.all()
	if len(all) != 1 {
		t.Fatalf("deliveries = %d", len(all))
	}
	h.log.audits = nil
	return all[0]
}

// A pass delivers what is due, retries with the approvals' backoff and
// gives up after MaxDeliveryAttempts, every attempt audited.
func TestDeliverOutputsRetriesAndGivesUp(t *testing.T) {
	h := newHarness()
	d := queuedDelivery(t, h, OutputDefinition{Name: "hook", Type: "webhook", URL: "https://receiver.example.test/", Secret: outputSecret})
	h.sender.failures = MaxDeliveryAttempts
	uc := NewDeliverOutputs(h.deps)
	ctx := context.Background()
	for attempt := 1; attempt <= MaxDeliveryAttempts; attempt++ {
		outcomes, err := uc.Execute(ctx)
		if err != nil || len(outcomes) != 1 || outcomes[0].Reason != "HTTP 503" {
			t.Fatalf("attempt %d: %+v %v", attempt, outcomes, err)
		}
		if nothing, _ := uc.Execute(ctx); len(nothing) != 0 && attempt < MaxDeliveryAttempts {
			t.Fatalf("attempt %d made again before its backoff", attempt)
		}
		h.clock.now = outcomes[0].Delivery.NextAttemptAt()
	}
	stored, _ := h.outbox2.Get(ctx, d.ID())
	if stored.Status() != domain.DeliveryFailed || stored.Attempts() != MaxDeliveryAttempts || len(h.sender.sent) != MaxDeliveryAttempts {
		t.Errorf("stored = %+v", stored.Snapshot())
	}
	last := h.log.audits[len(h.log.audits)-1]
	if last.Action != "incident.output" || last.Outcome != domain.AuditFailed || last.Details["attempt"] != "6" || last.Details["status"] != "failed" || last.Details["target"] != "hook" {
		t.Errorf("audit = %+v", last)
	}
	if first := h.log.audits[0]; first.Details["retryAt"] != t0.Add(10*time.Second).UTC().Format(time.RFC3339) {
		t.Errorf("first retry = %+v", first.Details)
	}
}

func TestDeliverOutputsDelivers(t *testing.T) {
	h := newHarness()
	d := queuedDelivery(t, h, OutputDefinition{Name: "chat", Type: "slack", URL: slackURL})
	outcomes, err := NewDeliverOutputs(h.deps).Execute(context.Background())
	if err != nil || len(outcomes) != 1 || outcomes[0].Delivery.Status() != domain.DeliveryDelivered || outcomes[0].Reason != "" {
		t.Fatalf("outcomes = %+v %v", outcomes, err)
	}
	if a := h.log.audits[0]; a.Outcome != domain.AuditOK || a.Details["status"] != "delivered" || a.Details["attempt"] != "1" || a.Subject != d.ID().String() {
		t.Errorf("audit = %+v", a)
	}
}

// A delivery whose target was removed, or replaced by another type, is
// skipped without an attempt.
func TestDeliverOutputsSkipsAGoneTarget(t *testing.T) {
	for name, replace := range map[string]*OutputDefinition{
		"removed":  nil,
		"replaced": {Name: "hook", Type: "command", Command: []string{"/bin/true"}},
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness()
			queuedDelivery(t, h, OutputDefinition{Name: "hook", Type: "webhook", URL: "https://receiver.example.test/", Secret: outputSecret})
			if _, err := NewRemoveOutput(h.deps).Execute(context.Background(), "hook"); err != nil {
				t.Fatal(err)
			}
			if replace != nil {
				addOutput(t, h, *replace)
			}
			outcomes, err := NewDeliverOutputs(h.deps).Execute(context.Background())
			if err != nil || len(outcomes) != 1 || outcomes[0].Delivery.Status() != domain.DeliverySkipped || outcomes[0].Reason != "the output hook is gone" {
				t.Fatalf("outcomes = %+v %v", outcomes, err)
			}
			if len(h.sender.sent) != 0 {
				t.Error("a skipped delivery was sent")
			}
		})
	}
}

func TestDeliverOutputsFailures(t *testing.T) {
	h := newHarness()
	queuedDelivery(t, h, OutputDefinition{Name: "hook", Type: "webhook", URL: "https://receiver.example.test/", Secret: outputSecret})
	ctx := context.Background()
	uc := NewDeliverOutputs(h.deps)
	h.outbox2.listFail = errBoom
	if _, err := uc.Execute(ctx); err == nil {
		t.Error("a list failure passed")
	}
	h.outbox2.listFail, h.outs.listFail = nil, errBoom
	if _, err := uc.Execute(ctx); err == nil {
		t.Error("a target read failure passed")
	}
	h.outs.listFail, h.outbox2.fail = nil, errBoom
	if _, err := uc.Execute(ctx); err == nil {
		t.Error("a save failure passed")
	}
	h.outbox2.fail, h.log.recordFail = nil, errBoom
	if _, err := uc.Execute(ctx); err == nil {
		t.Error("an audit failure passed")
	}
	h.log.recordFail = nil
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if outcomes, err := uc.Execute(cancelled); err != nil || len(outcomes) != 0 {
		t.Errorf("cancelled = %+v %v", outcomes, err)
	}
}

// The views never carry a URL, a secret or a command; the audit names
// the fingerprint.
func TestOutputUseCases(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	view := addOutput(t, h, OutputDefinition{Name: "chat", Type: "slack", URL: slackURL, Severities: []string{"sev1", "sev2"}})
	addOutput(t, h, OutputDefinition{Name: "relay", Type: "command", Command: []string{"/usr/local/bin/teams-relay", "--token", "a-token-in-an-argument"}})
	if view.Host != "hooks.slack.test" || !strings.HasPrefix(view.Fingerprint, "sha256:") || len(view.Events) != len(domain.DefaultOutputEvents) {
		t.Errorf("view = %+v", view)
	}
	views, err := NewListOutputs(h.deps).Execute(ctx)
	if err != nil || len(views) != 2 || views[0].Name != "chat" || views[1].Name != "relay" {
		t.Fatalf("views = %+v %v", views, err)
	}
	raw, _ := json.Marshal(views)
	for _, secret := range []string{"XXXX", "/services/", "a-token-in-an-argument", "teams-relay"} {
		if strings.Contains(string(raw), secret) {
			t.Errorf("the views show %q: %s", secret, raw)
		}
	}
	if _, err := NewAddOutput(h.deps).Execute(ctx, OutputDefinition{Name: "chat", Type: "slack", URL: slackURL}); err == nil {
		t.Error("a name added twice")
	}
	if _, err := NewAddOutput(h.deps).Execute(ctx, OutputDefinition{Name: "bad", Type: "slack"}); !isValidation(err) {
		t.Errorf("no url = %v", err)
	}
	removed, err := NewRemoveOutput(h.deps).Execute(ctx, "chat")
	if err != nil || removed.Fingerprint != view.Fingerprint {
		t.Errorf("removed = %+v %v", removed, err)
	}
	if _, err := NewRemoveOutput(h.deps).Execute(ctx, "chat"); !domain.IsNotFound(err) {
		t.Errorf("removed twice = %v", err)
	}
	if _, err := NewRemoveOutput(h.deps).Execute(ctx, "Bad Name"); !isValidation(err) {
		t.Errorf("bad name = %v", err)
	}
	actions := []string{}
	for _, a := range h.log.audits {
		actions = append(actions, a.Action)
		if a.Details["fingerprint"] == "" {
			t.Errorf("audit without a fingerprint: %+v", a)
		}
	}
	if strings.Join(actions, ",") != "output.add,output.add,output.remove" {
		t.Errorf("actions = %v", actions)
	}
	h.outs.listFail = errBoom
	if _, err := NewListOutputs(h.deps).Execute(ctx); err == nil {
		t.Error("a list failure passed")
	}
	if _, err := NewRemoveOutput(h.deps).Execute(ctx, "relay"); err == nil {
		t.Error("a read failure passed")
	}
	h.outs.listFail, h.outs.addFail = nil, errBoom
	if _, err := NewAddOutput(h.deps).Execute(ctx, OutputDefinition{Name: "x", Type: "slack", URL: slackURL}); err == nil {
		t.Error("an add failure passed")
	}
	h.outs.addFail, h.log.recordFail = nil, errBoom
	if _, err := NewAddOutput(h.deps).Execute(ctx, OutputDefinition{Name: "y", Type: "slack", URL: slackURL}); err == nil {
		t.Error("an audit failure passed")
	}
	if _, err := NewRemoveOutput(h.deps).Execute(ctx, "relay"); err == nil {
		t.Error("an audit failure passed")
	}
}

// The Slack message says each state in its own words: the cause once
// diagnosed, the proposal and "Needs you" when proposed, the end with
// its duration, an escalation's reason, and no button without a link.
func TestSlackIncidentMessage(t *testing.T) {
	base := IncidentOutputEvent{
		Event: "incident.escalated", IncidentID: "inc-9", TenantID: "shop", BotName: "triage", Title: "Disk <full> & more", Severity: "sev1",
		Status: "Escalated", Cycle: 2, Diagnosis: "PVC data-0 at 100 %", Reason: "the fix was denied twice", CostUSD: 0.125, CostUnknown: true,
	}
	raw, _ := json.Marshal(NewSlackIncidentMessage(base))
	golden(t, "slack-escalated", raw)
	if strings.Contains(string(raw), `"actions"`) {
		t.Error("a button without a link")
	}
	long := base
	long.Title = strings.Repeat("t", 200)
	if header := NewSlackIncidentMessage(long).Blocks[0].Text.Text; len([]rune(header)) != slackHeaderLimit {
		t.Errorf("header = %d characters", len([]rune(header)))
	}
	for seconds, want := range map[int64]string{45: "45s", 750: "12m 30s", 7500: "2h 5m"} {
		if got := slackDuration(seconds); got != want {
			t.Errorf("duration %d = %q, want %q", seconds, got, want)
		}
	}
	for _, event := range []domain.EventType{domain.EventIncidentRepeated, domain.EventIncidentReopened, domain.EventIncidentApplying, domain.EventIncidentVerifying, "other"} {
		if slackEventWords(event.String()) == "" {
			t.Errorf("no words for %s", event)
		}
	}
}

// The body's needsYou is the API's: the incident's current task waits
// for an approval where the incident stands; a proposal whose apply has
// not asked yet is not.
func TestOutputNeedsYouIsTheAPIs(t *testing.T) {
	h := newHarness()
	triage := brigade(t, h, domain.ApprovalsSnapshot{})
	ctx := context.Background()
	incident := proposed(t, h, triage)
	q := incidentOutputQueue{deps: h.deps, rec: recorder{deps: h.deps}}
	if q.body(ctx, domain.Event{Type: domain.EventIncidentProposed}, incident).NeedsYou {
		t.Error("a pending apply needs you")
	}
	waitingApply(t, h, incident)
	incident, _ = h.incs.Get(ctx, incident.ID())
	report, err := NewGetIncident(h.deps).Execute(ctx, incident.ID())
	if err != nil || !report.NeedsPerson || !q.body(ctx, domain.Event{Type: domain.EventIncidentProposed}, incident).NeedsYou {
		t.Errorf("a waiting apply: API %v, body %v", report.NeedsPerson, err)
	}
	if q.needsYou(ctx, domain.Incident{}) {
		t.Error("an incident without a task needs you")
	}
}

// What a bot wrote cannot break the message: escaped after the cut, so
// the section stays within Block Kit's limit; its backticks cannot close
// the fence; the fences stay whole; prose loses its marks.
func TestSlackMessageKeepsABotsTextInItsPlace(t *testing.T) {
	nasty := strings.Repeat("<!channel> & `x` ``` *b* ", MaxOutputExcerpt/24)
	message := NewSlackIncidentMessage(IncidentOutputEvent{
		Event: "incident.proposed", IncidentID: "inc-1", TenantID: "t", BotName: "fix", Title: "t", Severity: "sev2", Status: "Proposed",
		Diagnosis: strings.Repeat("&", 3000), Proposal: nasty,
	})
	var cause, proposal string
	for _, block := range message.Blocks {
		if block.Text == nil {
			continue
		}
		switch {
		case strings.HasPrefix(block.Text.Text, "*Cause*"):
			cause = block.Text.Text
		case strings.HasPrefix(block.Text.Text, "*Proposal*"):
			proposal = block.Text.Text
		}
	}
	for name, text := range map[string]string{"cause": cause, "proposal": proposal} {
		body := strings.TrimSuffix(strings.SplitN(text, "\n", 2)[1], "```")
		if n := len([]rune(text)); n > slackSectionLimit || !strings.HasSuffix(text, "```") || !strings.HasPrefix(body, "```") || strings.Contains(strings.TrimPrefix(body, "```"), "```") {
			t.Errorf("%s: %d characters, fences broken: %q…", name, n, text[:80])
		}
	}
	if !strings.Contains(cause, "&amp;…```") || strings.Contains(proposal, "<!channel>") || !strings.Contains(proposal, "&lt;!channel&gt; &amp; ˋxˋ") {
		t.Errorf("escaping: cause %q, proposal %q", cause[len(cause)-20:], proposal[:60])
	}
	if got := slackProse("> quoted *bold* `code` ~strike~\n>> again <@U1>"); got != "› quoted ∗bold∗ ˋcodeˋ ∼strike∼\n›&gt; again &lt;@U1&gt;" {
		t.Errorf("prose = %q", got)
	}
	if got := slackFit("abc", 3); got != "abc" {
		t.Errorf("fit exact = %q", got)
	}
	if got := slackFit("a&cd", 4); got != "a…" {
		t.Errorf("fit an entity = %q", got)
	}
}

func deliveryOfEvent(t *testing.T, h *harness, target domain.OutputTarget, incident domain.Incident, event string, kind domain.EventType, at time.Time) domain.OutputDelivery {
	t.Helper()
	d, err := domain.NewOutputDelivery(domain.DeliveryID("dlv-"+event), target, incident, domain.EventID(event), kind, []byte(`{}`), at)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.outbox2.Save(context.Background(), d); err != nil {
		t.Fatal(err)
	}
	return d
}

// A pass goes oldest first, holds an incident's later deliveries to a
// target while an earlier one is still pending, and tries a target that
// failed no more in the same pass.
func TestDeliverOutputsKeepsTheOrderAndStopsAtADeadTarget(t *testing.T) {
	h := newHarness()
	chat := addOutputTarget(t, h, OutputDefinition{Name: "chat", Type: "slack", URL: slackURL})
	other := addOutputTarget(t, h, OutputDefinition{Name: "other", Type: "slack", URL: slackURL})
	one, _ := domain.NewIncident("inc-1", "ops", "triage", "g1", "manual", "t", domain.Sev2, nil, t0)
	two, _ := domain.NewIncident("inc-2", "ops", "triage", "g2", "manual", "t", domain.Sev2, nil, t0)
	// Ids in the reverse of the events' order: the store's order is not theirs.
	deliveryOfEvent(t, h, chat, one, "z-opened", domain.EventIncidentOpened, t0)
	deliveryOfEvent(t, h, chat, one, "y-diagnosed", domain.EventIncidentDiagnosed, t0.Add(time.Second))
	deliveryOfEvent(t, h, chat, one, "x-resolved", domain.EventIncidentResolved, t0.Add(2*time.Second))
	deliveryOfEvent(t, h, chat, two, "w-opened", domain.EventIncidentOpened, t0.Add(3*time.Second))
	deliveryOfEvent(t, h, other, two, "v-opened", domain.EventIncidentOpened, t0.Add(4*time.Second))
	h.clock.now = t0.Add(time.Minute)
	uc := NewDeliverOutputs(h.deps)
	ctx := context.Background()
	outcomes, err := uc.Execute(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var order []string
	for _, o := range outcomes {
		order = append(order, o.Delivery.EventID().String())
	}
	if strings.Join(order, ",") != "z-opened,y-diagnosed,x-resolved,w-opened,v-opened" {
		t.Errorf("order = %v", order)
	}
	// chat fails: its first delivery retries, nothing else goes to it in
	// the pass, and the other target is still tried.
	for _, id := range []string{"z2", "y2", "x2"} {
		deliveryOfEvent(t, h, chat, one, id, domain.EventIncidentReopened, h.clock.now.Add(time.Duration(len(id))*time.Millisecond))
	}
	deliveryOfEvent(t, h, chat, two, "w2", domain.EventIncidentResolved, h.clock.now.Add(time.Second))
	deliveryOfEvent(t, h, other, two, "v2", domain.EventIncidentResolved, h.clock.now.Add(2*time.Second))
	h.sender.failures, h.sender.sent = 1, nil
	h.clock.now = h.clock.now.Add(time.Minute)
	outcomes, err = uc.Execute(ctx)
	if err != nil || len(outcomes) != 2 || outcomes[0].Delivery.Status() != domain.DeliveryPending || outcomes[1].Delivery.Output() != "other" {
		t.Fatalf("pass with chat down = %+v %v", outcomes, err)
	}
	// Next pass, before chat's retry is due: its later deliveries of that
	// incident wait behind it; another incident's go.
	h.clock.now = h.clock.now.Add(time.Second)
	outcomes, _ = uc.Execute(ctx)
	if len(outcomes) != 1 || outcomes[0].Delivery.EventID() != "w2" {
		t.Errorf("pass while the retry waits = %+v", outcomes)
	}
	h.clock.now = h.clock.now.Add(time.Minute)
	outcomes, _ = uc.Execute(ctx)
	if len(outcomes) != 3 {
		t.Errorf("pass after the retry = %d outcomes", len(outcomes))
	}
}

func addOutputTarget(t *testing.T, h *harness, definition OutputDefinition) domain.OutputTarget {
	t.Helper()
	addOutput(t, h, definition)
	target, err := h.outs.Get(context.Background(), domain.OutputName(definition.Name))
	if err != nil {
		t.Fatal(err)
	}
	return target
}
