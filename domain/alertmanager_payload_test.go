package domain

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// alertmanagerBody reads one of the recorded payloads of
// kube-prometheus-stack's Alertmanager (version 4).
func alertmanagerBody(t *testing.T, name string) []byte {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("testdata", "alertmanager-"+name+".json"))
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func TestAlertmanagerPayloadsAreRead(t *testing.T) {
	firing, err := ParseAlertmanagerPayload(alertmanagerBody(t, "firing"))
	if err != nil {
		t.Fatal(err)
	}
	if !firing.Firing() || firing.GroupKey != `{}/{alertname="KubePodCrashLooping"}:{alertname="KubePodCrashLooping", namespace="shop"}` || firing.Title() != "KubePodCrashLooping in shop" || firing.Severity() != Sev3 {
		t.Errorf("firing = %+v", firing)
	}
	if fps := firing.FiringFingerprints(); len(fps) != 1 || fps[0] != "9c6b1c1ef1b2d3a4" || len(firing.ResolvedFingerprints()) != 0 {
		t.Errorf("firing alerts = %v", fps)
	}
	repeat, err := ParseAlertmanagerPayload(alertmanagerBody(t, "repeat"))
	if err != nil || len(repeat.FiringFingerprints()) != 2 || repeat.GroupKey != firing.GroupKey {
		t.Errorf("repeat = %+v %v", repeat.FiringFingerprints(), err)
	}
	resolved, err := ParseAlertmanagerPayload(alertmanagerBody(t, "resolved"))
	if err != nil || resolved.Firing() || len(resolved.ResolvedFingerprints()) != 2 || len(resolved.FiringFingerprints()) != 0 {
		t.Errorf("resolved = %+v %v", resolved, err)
	}
	fields := firing.Fields()
	if fields["groupKey"] != firing.GroupKey || fields["alerts"].([]any)[0].(map[string]any)["fingerprint"] != "9c6b1c1ef1b2d3a4" {
		t.Errorf("fields = %v", fields)
	}
	for label, severity := range map[string]IncidentSeverity{"critical": Sev1, "page": Sev1, "error": Sev2, "warning": Sev3, "info": Sev4, "": Sev3, "weird": Sev3} {
		p := AlertmanagerPayload{CommonLabels: map[string]string{"severity": label}}
		if p.Severity() != severity {
			t.Errorf("severity %q = %s", label, p.Severity())
		}
	}
	bare := AlertmanagerPayload{GroupKey: "{}:{}", Alerts: []AlertmanagerAlert{{Labels: map[string]string{"alertname": "Up", "severity": "CRITICAL"}}}}
	if bare.Title() != "Up" || bare.Severity() != Sev1 {
		t.Errorf("bare = %q %s", bare.Title(), bare.Severity())
	}
	if (AlertmanagerPayload{GroupKey: "{}:{}"}).Title() != "{}:{}" {
		t.Error("a group without labels is not titled by its key")
	}
}

func TestAlertmanagerPayloadRefusals(t *testing.T) {
	good := string(alertmanagerBody(t, "firing"))
	bad := map[string]string{
		"not json":       "{",
		"a list":         "[]",
		"version 3":      replaceOnce(good, `"version":"4"`, `"version":"3"`),
		"no group key":   replaceOnce(good, `"groupKey":"{}/{alertname=\"KubePodCrashLooping\"}:{alertname=\"KubePodCrashLooping\", namespace=\"shop\"}"`, `"groupKey":" "`),
		"bad status":     replaceOnce(good, `"status":"firing","alerts"`, `"status":"pending","alerts"`),
		"no alerts":      replaceOnce(good, `"alerts":[{`, `"alerts":[],"x":[{`),
		"no fingerprint": replaceOnce(good, `"fingerprint":"9c6b1c1ef1b2d3a4"`, `"fingerprint":""`),
		"bad alert":      replaceOnce(good, `{"status":"firing","labels"`, `{"status":"gone","labels"`),
	}
	var verr ValidationError
	for name, body := range bad {
		if _, err := ParseAlertmanagerPayload([]byte(body)); !errors.As(err, &verr) {
			t.Errorf("%s: %v", name, err)
		}
	}
	trigger, err := NewTrigger(TriggerSnapshot{Type: "alertmanager", Name: "alerts", Secret: testSecret, Objective: "Look at {{.alert.groupKey}}"})
	if err != nil || trigger.Type() != TriggerAlertmanager || !trigger.Authenticates(testSecret, "", nil) || trigger.Authenticates("wrong", "", nil) {
		t.Errorf("alertmanager trigger: %v", err)
	}
	if _, err := NewTrigger(TriggerSnapshot{Type: "alertmanager", Name: "alerts"}); err == nil {
		t.Error("an alertmanager trigger without a credential validated")
	}
	if _, err := NewTrigger(TriggerSnapshot{Type: "alertmanager", Name: "alerts", Secret: testSecret, Every: "1m"}); err == nil {
		t.Error("an alertmanager trigger with a schedule validated")
	}
}

func replaceOnce(s, old, with string) string {
	for n := 0; n+len(old) <= len(s); n++ {
		if s[n:n+len(old)] == old {
			return s[:n] + with + s[n+len(old):]
		}
	}
	panic("replaceOnce: " + old + " not in the body")
}

func TestIncidentMarksResolvedAlerts(t *testing.T) {
	i := sampleIncident(t) // fp-a, fp-b
	if err := i.MarkResolved([]string{"fp-a", "fp-new"}, t0); err != nil || len(i.ResolvedFingerprints()) != 2 || len(i.Fingerprints()) != 3 {
		t.Fatalf("mark: %v %+v", err, i.Snapshot())
	}
	if firing := i.FiringFingerprints(); len(firing) != 1 || firing[0] != "fp-b" {
		t.Errorf("firing = %v", firing)
	}
	// An alert that fires again is no longer resolved.
	if err := i.Repeat([]string{"fp-a"}, t0); err != nil || len(i.ResolvedFingerprints()) != 1 || i.ResolvedFingerprints()[0] != "fp-new" {
		t.Errorf("repeat: %v %v", err, i.ResolvedFingerprints())
	}
	back, err := RestoreIncident(i.Snapshot())
	if err != nil || len(back.ResolvedFingerprints()) != 1 {
		t.Errorf("restore: %v", err)
	}
	broken := i.Snapshot()
	broken.ResolvedFingerprints = []string{"fp-unseen"}
	if _, err := RestoreIncident(broken); err == nil {
		t.Error("a resolved alert never seen restored")
	}
	_ = i.Resolve("", t0)
	if err := i.Reopen([]string{"fp-new"}, t0.Add(time.Minute)); err != nil || len(i.ResolvedFingerprints()) != 0 {
		t.Errorf("reopen: %v %v", err, i.ResolvedFingerprints())
	}
}
