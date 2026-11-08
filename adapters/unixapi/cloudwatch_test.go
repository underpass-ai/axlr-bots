package unixapi

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
	"github.com/underpass-ai/axlr-bots/internal/snstest"
)

// The SNS bodies are composed from AWS's documented formats, not
// captured (domain/testdata/sns-*); the tests re-sign them with a key
// they make and serve its certificate through a fake transport.

func snsFake(t *testing.T) (*snstest.Transport, func(*application.Dependencies)) {
	t.Helper()
	signer, err := snstest.NewSigner(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	fake := &snstest.Transport{Signer: signer}
	return fake, func(d *application.Dependencies) { d.SNS = fake }
}

func snsBody(t *testing.T, fake *snstest.Transport, name string, edit func(*domain.SNSMessage)) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "domain", "testdata", "sns-"+name+".json"))
	if err != nil {
		t.Fatal(err)
	}
	if fake == nil {
		return raw
	}
	body, err := fake.Signer.Sign(raw, func(m *domain.SNSMessage) {
		snstest.At(time.Now())(m)
		if edit != nil {
			edit(m)
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func cloudWatchDefinition(autoConfirm bool) application.BotDefinition {
	d := definition("triage", "observe")
	d.Triggers = []domain.TriggerSnapshot{{Type: "cloudwatch", Name: "cw", Objective: "Alarm {{.alarmName}}: {{.reason}}", Severity: "sev3", AutoConfirm: autoConfirm, Topics: []string{"arn:aws:sns:eu-west-1:123456789012:axlr-sre-alarms"}}}
	return d
}

func TestCloudWatchThroughSNSOnTheOperatorAddress(t *testing.T) {
	fake, change := snsFake(t)
	o := serveOperatorAddressWith(t, change)
	ctx := context.Background()
	if _, err := o.socket.RegisterBot(ctx, cloudWatchDefinition(false)); err != nil {
		t.Fatal(err)
	}
	if _, err := o.socket.EnableBot(ctx, "triage"); err != nil {
		t.Fatal(err)
	}
	sns := func(kind string) map[string]string {
		return map[string]string{SNSMessageTypeHeader: kind, "Content-Type": "text/plain; charset=UTF-8", "User-Agent": "Amazon Simple Notification Service Agent"}
	}
	path := "/v1/triggers/triage/cw"
	status, confirmation := o.post(t, path, sns(domain.SNSSubscriptionConfirmation), snsBody(t, fake, "subscription-confirmation", nil))
	if status != http.StatusOK || confirmation["action"] != "ignored" || len(fake.Confirmed()) != 0 {
		t.Errorf("confirmation: %d %v", status, confirmation)
	}
	status, opened := o.post(t, path, sns(domain.SNSNotification), snsBody(t, fake, "cloudwatch-alarm", nil))
	if status != http.StatusCreated || opened["action"] != "opened" || opened["incident"].(map[string]any)["source"] != "cloudwatch" || opened["task"] == nil {
		t.Fatalf("alarm: %d %v", status, opened)
	}
	status, repeated := o.post(t, path, sns(domain.SNSNotification), snsBody(t, fake, "cloudwatch-alarm-again", nil))
	if status != http.StatusOK || repeated["action"] != "repeated" || repeated["task"] != nil {
		t.Errorf("again: %d %v", status, repeated)
	}
	status, resolved := o.post(t, path, sns(domain.SNSNotification), snsBody(t, fake, "cloudwatch-ok", nil))
	if status != http.StatusOK || resolved["action"] != "resolved" || resolved["incident"].(map[string]any)["status"] != "Resolved" {
		t.Errorf("ok: %d %v", status, resolved)
	}
	status, reopened := o.post(t, path, sns(domain.SNSNotification), snsBody(t, fake, "cloudwatch-alarm", nil))
	if status != http.StatusOK || reopened["action"] != "reopened" || reopened["task"] == nil {
		t.Errorf("reopened: %d %v", status, reopened)
	}
	if status, ignored := o.post(t, path, sns(domain.SNSNotification), snsBody(t, fake, "cloudwatch-insufficient", nil)); status != http.StatusOK || ignored["action"] != "ignored" {
		t.Errorf("insufficient: %d %v", status, ignored)
	}
	if status, gone := o.post(t, path, sns(domain.SNSUnsubscribeConfirmation), snsBody(t, fake, "unsubscribe-confirmation", nil)); status != http.StatusOK || gone["action"] != "ignored" {
		t.Errorf("unsubscribe: %d %v", status, gone)
	}

	// The signature is the credential: no secret header opens it.
	if status, decoded := o.post(t, path, sns(domain.SNSNotification), snsBody(t, nil, "cloudwatch-alarm", nil)); status != http.StatusForbidden || decoded["kind"] != KindForbidden {
		t.Errorf("unsigned: %d %v", status, decoded)
	}
	offHost := snsBody(t, fake, "cloudwatch-alarm", func(m *domain.SNSMessage) { m.SigningCertURL = "https://attacker.example/cert.pem" })
	if status, _ := o.post(t, path, sns(domain.SNSNotification), offHost); status != http.StatusForbidden {
		t.Errorf("certificate off SNS: %d", status)
	}
	if status, decoded := o.post(t, path, sns(domain.SNSNotification), []byte(`{"type": "deploy"}`)); status != http.StatusBadRequest || decoded["kind"] != KindValidation {
		t.Errorf("not SNS: %d %v", status, decoded)
	}
	if status, _ := o.post(t, path, sns(domain.SNSSubscriptionConfirmation), snsBody(t, fake, "cloudwatch-alarm", nil)); status != http.StatusBadRequest {
		t.Errorf("header against body: %d", status)
	}
	if status, _ := o.post(t, "/v1/triggers/triage/other", sns(domain.SNSNotification), snsBody(t, fake, "cloudwatch-alarm", nil)); status != http.StatusForbidden {
		t.Errorf("unknown trigger: %d", status)
	}
	stale := snsBody(t, fake, "cloudwatch-alarm", snstest.At(time.Now().Add(-domain.SNSMaxMessageAge-time.Minute)))
	if status, decoded := o.post(t, path, sns(domain.SNSNotification), stale); status != http.StatusForbidden || decoded["kind"] != KindForbidden {
		t.Errorf("replayed: %d %v", status, decoded)
	}
	ahead := snsBody(t, fake, "cloudwatch-alarm", snstest.At(time.Now().Add(domain.SNSMaxMessageAge+time.Minute)))
	if status, _ := o.post(t, path, sns(domain.SNSNotification), ahead); status != http.StatusForbidden {
		t.Errorf("stamped ahead: %d", status)
	}
	otherTopic := snsBody(t, fake, "cloudwatch-alarm", func(m *domain.SNSMessage) { m.TopicArn = "arn:aws:sns:eu-west-1:999999999999:axlr-sre-alarms" })
	if status, decoded := o.post(t, path, sns(domain.SNSNotification), otherTopic); status != http.StatusForbidden || decoded["kind"] != KindForbidden {
		t.Errorf("another topic: %d %v", status, decoded)
	}
	incidents, err := o.api.ListIncidents(ctx, application.IncidentQuery{Tenant: "ops"})
	if err != nil || len(incidents) != 1 || incidents[0].Cycle != 2 {
		t.Errorf("incidents = %+v %v", incidents, err)
	}
}

func TestCloudWatchOverTheSocketIsVerifiedToo(t *testing.T) {
	fake, change := snsFake(t)
	client, _, _ := serveWith(t, "completed", change)
	ctx := context.Background()
	unlisted := cloudWatchDefinition(true)
	unlisted.Triggers[0].Topics = nil
	if _, err := client.RegisterBot(ctx, unlisted); remoteKind(err) != KindValidation {
		t.Errorf("autoConfirm without topics: %v", err)
	}
	if _, err := client.RegisterBot(ctx, cloudWatchDefinition(true)); err != nil {
		t.Fatal(err)
	}
	if _, err := client.EnableBot(ctx, "triage"); err != nil {
		t.Fatal(err)
	}
	confirmed, err := client.FireTrigger(ctx, FireRequest{BotID: "triage", Trigger: "cw", MessageType: domain.SNSSubscriptionConfirmation, Body: snsBody(t, fake, "subscription-confirmation", nil)})
	if err != nil || confirmed.Action != "confirmed" || len(fake.Confirmed()) != 1 {
		t.Errorf("confirmed = %+v %v", confirmed, err)
	}
	var forbidden domain.ForbiddenError
	if _, err := client.FireTrigger(ctx, FireRequest{BotID: "triage", Trigger: "cw", Body: snsBody(t, nil, "cloudwatch-alarm", nil)}); !errors.As(err, &forbidden) {
		t.Errorf("unsigned over the socket: %v", err)
	}
	opened, err := client.FireTrigger(ctx, FireRequest{BotID: "triage", Trigger: "cw", Body: snsBody(t, fake, "cloudwatch-alarm", nil)})
	if err != nil || opened.Action != "opened" || opened.Task == nil || opened.Task.Objective == "" {
		t.Errorf("opened = %+v %v", opened, err)
	}
}
