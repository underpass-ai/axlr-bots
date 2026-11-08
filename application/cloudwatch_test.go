package application

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/underpass-ai/axlr-bots/domain"
	"github.com/underpass-ai/axlr-bots/internal/snstest"
)

// The SNS bodies are composed from AWS's documented formats, not
// captured (domain/testdata/sns-*); each test re-signs them with a key it
// makes, and the harness's fake transport serves that key's certificate.

func cloudWatchBot(t *testing.T, h *harness, trigger domain.TriggerSnapshot) domain.Bot {
	t.Helper()
	ctx := context.Background()
	if h.sns.Signer == nil {
		signer, err := snstest.NewSigner(time.Now())
		if err != nil {
			t.Fatal(err)
		}
		h.sns.Signer = signer
	}
	definition := sampleDefinition()
	definition.Name = "triage"
	trigger.Type, trigger.Name = "cloudwatch", "cw"
	definition.Triggers = []domain.TriggerSnapshot{trigger}
	bot, err := NewRegisterBot(h.deps).Execute(ctx, definition)
	if err != nil {
		t.Fatal(err)
	}
	if bot, err = NewEnableBot(h.deps).Execute(ctx, bot.ID()); err != nil {
		t.Fatal(err)
	}
	h.log.events, h.log.audits = nil, nil
	return bot
}

// snsSigned is the recorded body, signed by the harness's key.
func snsSigned(t *testing.T, h *harness, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "domain", "testdata", "sns-"+name+".json"))
	if err != nil {
		t.Fatal(err)
	}
	body, err := h.sns.Signer.Sign(raw, snstest.At(h.clock.now))
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func fireSNS(t *testing.T, h *harness, bot domain.Bot, body []byte, messageType string) FireTriggerResult {
	t.Helper()
	result, err := NewFireTrigger(h.deps).Execute(context.Background(), FireTriggerCommand{BotID: bot.ID().String(), TriggerName: "cw", Body: body, MessageType: messageType})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func lastAudit(h *harness, action string) (domain.AuditEntry, bool) {
	for n := len(h.log.audits) - 1; n >= 0; n-- {
		if h.log.audits[n].Action == action {
			return h.log.audits[n], true
		}
	}
	return domain.AuditEntry{}, false
}

func TestCloudWatchAlarmIsOneIncidentThroughItsLife(t *testing.T) {
	h := newHarness()
	bot := cloudWatchBot(t, h, domain.TriggerSnapshot{Objective: "Alarm {{.alarmName}}: {{.reason}}", Severity: "sev2"})
	first := fireSNS(t, h, bot, snsSigned(t, h, "cloudwatch-alarm"), domain.SNSNotification)
	if first.Action != IncidentOpened || !first.Created || first.Task == nil || first.Incident == nil {
		t.Fatalf("first = %+v", first)
	}
	incident := *first.Incident
	if incident.Source() != CloudWatchSource || incident.GroupKey() != "cloudwatch:arn:aws:cloudwatch:eu-west-1:123456789012:alarm:shop-web-5xx" || incident.Severity() != domain.Sev2 ||
		!strings.HasPrefix(incident.Title(), "shop-web-5xx: Threshold Crossed") || len(incident.Fingerprints()) != 1 || incident.Fingerprints()[0] != "2026-11-08T10:03:12.402+0000" {
		t.Errorf("incident = %+v", incident.Snapshot())
	}
	task := *first.Task
	if task.Trigger() != (domain.TriggerRef{Type: domain.TriggerCloudWatch, Name: "cw"}) || !strings.HasPrefix(task.Objective().String(), "Alarm shop-web-5xx: Threshold Crossed") || !strings.Contains(string(task.Input()), `"AlarmName":"shop-web-5xx"`) || strings.Contains(string(task.Input()), "SigningCertURL") {
		t.Errorf("task = %+v", task.Snapshot())
	}
	if got := h.log.events[len(h.log.events)-1]; got.Type != domain.EventTriggerFired || got.Payload["action"] != IncidentOpened || got.Payload["state"] != "ALARM" || got.Payload["messageType"] != domain.SNSNotification {
		t.Errorf("event = %+v", got)
	}
	// SNS retries the same message: a repeat, no task.
	again := fireSNS(t, h, bot, snsSigned(t, h, "cloudwatch-alarm"), "")
	if again.Action != IncidentRepeated || again.Task != nil || again.Incident.ID() != incident.ID() {
		t.Errorf("again = %+v", again)
	}
	// The alarm fires again (a new StateChangeTime): the same incident.
	newer := fireSNS(t, h, bot, snsSigned(t, h, "cloudwatch-alarm-again"), domain.SNSNotification)
	if newer.Action != IncidentRepeated || newer.Task != nil || len(newer.Incident.Fingerprints()) != 2 || newer.Incident.Repeats() != 2 {
		t.Errorf("newer = %+v", newer.Incident.Snapshot())
	}
	// INSUFFICIENT_DATA changes nothing and is audited.
	unknown := fireSNS(t, h, bot, snsSigned(t, h, "cloudwatch-insufficient"), domain.SNSNotification)
	if unknown.Action != IncidentIgnored || unknown.Incident != nil || unknown.Task != nil {
		t.Errorf("insufficient = %+v", unknown)
	}
	if entry, ok := lastAudit(h, "trigger.fire"); !ok || !strings.Contains(entry.Details["ignored"], "INSUFFICIENT_DATA") {
		t.Errorf("audit = %+v", entry)
	}
	// OK resolves the incident, every alarm of it, and starts nothing.
	ok := fireSNS(t, h, bot, snsSigned(t, h, "cloudwatch-ok"), domain.SNSNotification)
	if ok.Action != IncidentClosed || ok.Task != nil || ok.Incident.Status() != domain.IncidentResolved || len(ok.Incident.ResolvedFingerprints()) != 2 || len(ok.Incident.Fingerprints()) != 2 {
		t.Fatalf("ok = %+v", ok.Incident.Snapshot())
	}
	// A second OK: nothing open, ignored.
	if twice := fireSNS(t, h, bot, snsSigned(t, h, "cloudwatch-ok"), ""); twice.Action != IncidentIgnored || twice.Incident != nil {
		t.Errorf("second ok = %+v", twice)
	}
	// ALARM within the reopen window: the same incident, cycle 2, a task.
	h.clock.now = h.clock.now.Add(10 * time.Minute)
	reopened := fireSNS(t, h, bot, snsSigned(t, h, "cloudwatch-alarm-again"), domain.SNSNotification)
	if reopened.Action != IncidentReopened || reopened.Task == nil || reopened.Incident.ID() != incident.ID() || reopened.Incident.Cycle() != 2 {
		t.Errorf("reopened = %+v", reopened)
	}
	fireSNS(t, h, bot, snsSigned(t, h, "cloudwatch-ok"), "")
	// ALARM past the window: a new incident.
	h.clock.now = h.clock.now.Add(domain.IncidentReopenWindow + time.Minute)
	later := fireSNS(t, h, bot, snsSigned(t, h, "cloudwatch-alarm"), domain.SNSNotification)
	if later.Action != IncidentOpened || later.Task == nil || later.Incident.ID() == incident.ID() || later.Incident.Cycle() != 1 {
		t.Errorf("later = %+v", later)
	}
}

func TestCloudWatchSubscriptionIsConfirmedByAPersonByDefault(t *testing.T) {
	h := newHarness()
	bot := cloudWatchBot(t, h, domain.TriggerSnapshot{})
	result := fireSNS(t, h, bot, snsSigned(t, h, "subscription-confirmation"), domain.SNSSubscriptionConfirmation)
	if result.Action != IncidentIgnored || result.Incident != nil || len(h.sns.Confirmed()) != 0 {
		t.Errorf("result = %+v, confirmed %v", result, h.sns.Confirmed())
	}
	entry, ok := lastAudit(h, "trigger.subscription")
	if !ok || !strings.HasPrefix(entry.Details["subscribeUrl"], "https://sns.eu-west-1.amazonaws.com/?Action=ConfirmSubscription") || entry.Details["autoConfirm"] != "false" || entry.Details["topicArn"] != "arn:aws:sns:eu-west-1:123456789012:axlr-sre-alarms" {
		t.Errorf("audit = %+v", entry)
	}
	unsubscribed := fireSNS(t, h, bot, snsSigned(t, h, "unsubscribe-confirmation"), domain.SNSUnsubscribeConfirmation)
	if unsubscribed.Action != IncidentIgnored {
		t.Errorf("unsubscribe = %+v", unsubscribed)
	}
	if _, ok := lastAudit(h, "trigger.unsubscription"); !ok {
		t.Error("the unsubscription is not audited")
	}
}

func TestCloudWatchAutoConfirm(t *testing.T) {
	h := newHarness()
	bot := cloudWatchBot(t, h, domain.TriggerSnapshot{AutoConfirm: true, Topics: []string{"arn:aws:sns:eu-west-1:123456789012:axlr-sre-alarms"}})
	body := snsSigned(t, h, "subscription-confirmation")
	result := fireSNS(t, h, bot, body, domain.SNSSubscriptionConfirmation)
	if result.Action != IncidentConfirmed || len(h.sns.Confirmed()) != 1 || !strings.Contains(h.sns.Confirmed()[0], "Action=ConfirmSubscription") {
		t.Errorf("result = %+v, confirmed %v", result, h.sns.Confirmed())
	}
	if entry, ok := lastAudit(h, "trigger.subscription.confirm"); !ok || entry.Outcome != domain.AuditOK {
		t.Errorf("audit = %+v", entry)
	}
	h.sns.ConfirmFail = errBoom
	if _, err := NewFireTrigger(h.deps).Execute(context.Background(), FireTriggerCommand{BotID: bot.ID().String(), TriggerName: "cw", Body: body}); !errors.Is(err, errBoom) {
		t.Errorf("a failed confirmation: %v", err)
	}
	if entry, ok := lastAudit(h, "trigger.subscription.confirm"); !ok || entry.Outcome != domain.AuditFailed {
		t.Errorf("audit = %+v", entry)
	}
}

func TestCloudWatchRefusals(t *testing.T) {
	h := newHarness()
	bot := cloudWatchBot(t, h, domain.TriggerSnapshot{})
	ctx := context.Background()
	fire := func(body []byte, messageType string) error {
		_, err := NewFireTrigger(h.deps).Execute(ctx, FireTriggerCommand{BotID: bot.ID().String(), TriggerName: "cw", Body: body, MessageType: messageType})
		return err
	}
	var forbidden domain.ForbiddenError
	var validation domain.ValidationError
	raw, _ := os.ReadFile(filepath.Join("..", "domain", "testdata", "sns-cloudwatch-alarm.json"))
	if err := fire(raw, ""); !errors.As(err, &forbidden) {
		t.Errorf("the recorded placeholder signature: %v", err)
	}
	stranger, err := snstest.NewSigner(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	forged, _ := stranger.Sign(raw, snstest.At(h.clock.now))
	if err := fire(forged, ""); !errors.As(err, &forbidden) {
		t.Errorf("another key: %v", err)
	}
	offHost, _ := h.sns.Signer.Sign(raw, func(m *domain.SNSMessage) { m.SigningCertURL = "https://evil.example/cert.pem"; snstest.At(h.clock.now)(m) })
	fetched := len(h.sns.Fetched())
	if err := fire(offHost, ""); !errors.As(err, &forbidden) || len(h.sns.Fetched()) != fetched {
		t.Errorf("a certificate off SNS: %v, fetched %v", err, h.sns.Fetched())
	}
	if err := fire(snsSigned(t, h, "cloudwatch-alarm"), domain.SNSSubscriptionConfirmation); !errors.As(err, &validation) {
		t.Errorf("a header that disagrees: %v", err)
	}
	if err := fire([]byte(`{"version":"4"}`), ""); !errors.As(err, &validation) {
		t.Errorf("not SNS: %v", err)
	}
	notAlarm, _ := h.sns.Signer.Sign(raw, func(m *domain.SNSMessage) { m.Message = "hello"; snstest.At(h.clock.now)(m) })
	if err := fire(notAlarm, ""); !errors.As(err, &validation) {
		t.Errorf("a message that is not an alarm: %v", err)
	}
	if entry, ok := lastAudit(h, "trigger.fire"); !ok || entry.Outcome != domain.AuditDenied {
		t.Errorf("audit = %+v", entry)
	}
	h.sns.Fail = errBoom
	if err := fire(snsSigned(t, h, "cloudwatch-alarm"), ""); !errors.Is(err, errBoom) || errors.As(err, &forbidden) {
		t.Errorf("a certificate that cannot be fetched: %v", err)
	}
	if len(h.incs.incidents) != 0 {
		t.Errorf("incidents = %d", len(h.incs.incidents))
	}
}

func TestCloudWatchRefusesReplaysAndOtherTopics(t *testing.T) {
	h := newHarness()
	bot := cloudWatchBot(t, h, domain.TriggerSnapshot{Topics: []string{"arn:aws:sns:eu-west-1:123456789012:axlr-sre-alarms"}})
	ctx := context.Background()
	raw, _ := os.ReadFile(filepath.Join("..", "domain", "testdata", "sns-cloudwatch-alarm.json"))
	var forbidden domain.ForbiddenError
	for name, edit := range map[string]func(*domain.SNSMessage){
		"replayed":      snstest.At(h.clock.now.Add(-domain.SNSMaxMessageAge - time.Minute)),
		"in the future": snstest.At(h.clock.now.Add(domain.SNSMaxMessageAge + time.Minute)),
		"another topic": func(m *domain.SNSMessage) {
			snstest.At(h.clock.now)(m)
			m.TopicArn = "arn:aws:sns:eu-west-1:999999999999:axlr-sre-alarms"
		},
	} {
		body, err := h.sns.Signer.Sign(raw, edit)
		if err != nil {
			t.Fatal(err)
		}
		h.log.audits = nil
		fetched := len(h.sns.Fetched())
		_, err = NewFireTrigger(h.deps).Execute(ctx, FireTriggerCommand{BotID: bot.ID().String(), TriggerName: "cw", Body: body})
		if !errors.As(err, &forbidden) || len(h.sns.Fetched()) != fetched {
			t.Errorf("%s: %v, fetched %v", name, err, h.sns.Fetched())
		}
		if entry, ok := lastAudit(h, "trigger.fire"); !ok || entry.Outcome != domain.AuditDenied || entry.Details["reason"] != err.Error() {
			t.Errorf("%s: audit = %+v", name, entry)
		}
	}
	if len(h.incs.incidents) != 0 {
		t.Errorf("incidents = %d", len(h.incs.incidents))
	}
	// A listed topic, stamped now, still opens one.
	if result := fireSNS(t, h, bot, snsSigned(t, h, "cloudwatch-alarm"), domain.SNSNotification); result.Action != IncidentOpened {
		t.Errorf("listed topic: %+v", result)
	}
}

func TestCloudWatchAutoConfirmNeedsTopics(t *testing.T) {
	h := newHarness()
	definition := sampleDefinition()
	definition.Triggers = []domain.TriggerSnapshot{{Type: "cloudwatch", Name: "cw", AutoConfirm: true}}
	var validation domain.ValidationError
	if _, err := NewRegisterBot(h.deps).Execute(context.Background(), definition); !errors.As(err, &validation) || !strings.Contains(err.Error(), "autoConfirm needs topics") {
		t.Errorf("register: %v", err)
	}
	bot := cloudWatchBot(t, h, domain.TriggerSnapshot{})
	patch := []domain.TriggerSnapshot{{Type: "cloudwatch", Name: "cw", AutoConfirm: true}}
	if _, err := NewUpdateBot(h.deps).Execute(context.Background(), bot.ID(), BotPatch{Triggers: &patch}); !errors.As(err, &validation) {
		t.Errorf("update: %v", err)
	}
	patch[0].Topics = []string{"arn:aws:sns:eu-west-1:123456789012:axlr-sre-alarms"}
	if _, err := NewUpdateBot(h.deps).Execute(context.Background(), bot.ID(), BotPatch{Triggers: &patch}); err != nil {
		t.Errorf("update with topics: %v", err)
	}
}
