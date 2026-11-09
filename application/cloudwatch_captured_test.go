package application

import (
	"strings"
	"testing"

	"github.com/underpass-ai/axlr-bots/domain"
)

// sns-captured-* are what a real CloudWatch alarm sent through a real SNS
// topic on 9 November 2026, read from an SQS subscription, with the account
// number replaced and the signature a placeholder; the original bodies
// verified against Amazon's certificate on a control plane of that day.
func TestACapturedCloudWatchAlarmOpensAndResolvesItsIncident(t *testing.T) {
	h := newHarness()
	bot := cloudWatchBot(t, h, domain.TriggerSnapshot{Objective: "Alarm {{.alarmName}}: {{.reason}}", Severity: "sev3"})

	opened := fireSNS(t, h, bot, snsSigned(t, h, "captured-alarm"), domain.SNSNotification)
	if opened.Action != IncidentOpened || opened.Incident == nil || opened.Task == nil {
		t.Fatalf("opened = %+v", opened)
	}
	incident := *opened.Incident
	if incident.GroupKey() != "cloudwatch:arn:aws:cloudwatch:eu-west-1:123456789012:alarm:axlr-sre-test-probe" {
		t.Errorf("group key = %q", incident.GroupKey())
	}
	if incident.Title() != "axlr-sre-test-probe: axlr-sre test: forced alarm" {
		t.Errorf("title = %q", incident.Title())
	}
	if got := incident.Fingerprints(); len(got) != 1 || got[0] != "2026-11-09T00:08:23.076+0000" {
		t.Errorf("fingerprints = %v", got)
	}
	if input := string(opened.Task.Input()); !strings.Contains(input, `"AlarmName":"axlr-sre-test-probe"`) || strings.Contains(input, "SigningCertURL") {
		t.Errorf("input = %s", input)
	}

	resolved := fireSNS(t, h, bot, snsSigned(t, h, "captured-ok"), domain.SNSNotification)
	if resolved.Action != IncidentClosed || resolved.Task != nil || resolved.Incident == nil || resolved.Incident.ID() != incident.ID() {
		t.Errorf("resolved = %+v", resolved)
	}
}
