package domain

import (
	"encoding/json"
	"strings"
	"time"
)

// Alertmanager's webhook payload, version 4: what the alertmanager
// trigger takes. A group is one incident; its alerts are the incident's
// fingerprints; the group's status says whether it fires or went green.
const (
	AlertmanagerVersion = "4"
	// AlertmanagerFiring and AlertmanagerResolved are the two statuses of
	// a group and of an alert.
	AlertmanagerFiring    = "firing"
	AlertmanagerResolved  = "resolved"
	maxAlertmanagerAlerts = 1000
)

// AlertmanagerAlert is one alert of the payload.
type AlertmanagerAlert struct {
	Status       string            `json:"status"`
	Labels       map[string]string `json:"labels"`
	Annotations  map[string]string `json:"annotations"`
	StartsAt     time.Time         `json:"startsAt"`
	EndsAt       time.Time         `json:"endsAt"`
	GeneratorURL string            `json:"generatorURL"`
	Fingerprint  string            `json:"fingerprint"`
}

// AlertmanagerPayload is the webhook's body as Alertmanager sends it.
type AlertmanagerPayload struct {
	Version           string              `json:"version"`
	GroupKey          string              `json:"groupKey"`
	TruncatedAlerts   int                 `json:"truncatedAlerts"`
	Status            string              `json:"status"`
	Receiver          string              `json:"receiver"`
	GroupLabels       map[string]string   `json:"groupLabels"`
	CommonLabels      map[string]string   `json:"commonLabels"`
	CommonAnnotations map[string]string   `json:"commonAnnotations"`
	ExternalURL       string              `json:"externalURL"`
	Alerts            []AlertmanagerAlert `json:"alerts"`
}

// ParseAlertmanagerPayload reads and checks a webhook body: version 4, a
// group key, a status, and at least one alert with a fingerprint and a
// status.
func ParseAlertmanagerPayload(body []byte) (AlertmanagerPayload, error) {
	var payload AlertmanagerPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return AlertmanagerPayload{}, invalid("body", "is not Alertmanager's webhook payload: "+err.Error())
	}
	switch {
	case payload.Version != AlertmanagerVersion:
		return AlertmanagerPayload{}, invalid("version", "must be "+AlertmanagerVersion+", Alertmanager's webhook payload")
	case strings.TrimSpace(payload.GroupKey) == "":
		return AlertmanagerPayload{}, invalid("groupKey", "must not be empty")
	case payload.Status != AlertmanagerFiring && payload.Status != AlertmanagerResolved:
		return AlertmanagerPayload{}, invalid("status", "must be firing or resolved")
	case len(payload.Alerts) == 0:
		return AlertmanagerPayload{}, invalid("alerts", "must not be empty")
	case len(payload.Alerts) > maxAlertmanagerAlerts:
		return AlertmanagerPayload{}, invalid("alerts", "a payload carries at most 1000 alerts")
	}
	for n, alert := range payload.Alerts {
		if strings.TrimSpace(alert.Fingerprint) == "" {
			return AlertmanagerPayload{}, invalid("alerts", "alert "+itoa(n)+" has no fingerprint")
		}
		if alert.Status != AlertmanagerFiring && alert.Status != AlertmanagerResolved {
			return AlertmanagerPayload{}, invalid("alerts", "alert "+itoa(n)+" must be firing or resolved")
		}
	}
	return payload, nil
}

// Firing reports a group that still fires.
func (p AlertmanagerPayload) Firing() bool { return p.Status == AlertmanagerFiring }

// FiringFingerprints lists the alerts that fire, in the payload's order.
func (p AlertmanagerPayload) FiringFingerprints() []string { return p.fingerprints(AlertmanagerFiring) }

// ResolvedFingerprints lists the alerts that went green.
func (p AlertmanagerPayload) ResolvedFingerprints() []string {
	return p.fingerprints(AlertmanagerResolved)
}

func (p AlertmanagerPayload) fingerprints(status string) []string {
	var out []string
	for _, alert := range p.Alerts {
		if alert.Status == status {
			out = append(out, strings.TrimSpace(alert.Fingerprint))
		}
	}
	return out
}

// Title is the group in one line: the alert name and the namespace of
// the group's or the common labels, else the group key.
func (p AlertmanagerPayload) Title() string {
	name := p.label("alertname")
	if name == "" {
		return p.GroupKey
	}
	if namespace := p.label("namespace"); namespace != "" {
		return name + " in " + namespace
	}
	return name
}

// Severity maps the severity label to an incident's: critical is sev1,
// error sev2, warning sev3, info sev4; anything else, or none, sev3.
func (p AlertmanagerPayload) Severity() IncidentSeverity {
	switch strings.ToLower(p.label("severity")) {
	case "critical", "page":
		return Sev1
	case "error", "major":
		return Sev2
	case "info", "none":
		return Sev4
	}
	return Sev3
}

// label reads a label of the group, of the common labels, or of the
// first alert, in that order.
func (p AlertmanagerPayload) label(name string) string {
	if value := p.GroupLabels[name]; value != "" {
		return value
	}
	if value := p.CommonLabels[name]; value != "" {
		return value
	}
	if len(p.Alerts) > 0 {
		return p.Alerts[0].Labels[name]
	}
	return ""
}

// Fields is the payload as the data an objective template renders over,
// under "alert".
func (p AlertmanagerPayload) Fields() map[string]any {
	raw, _ := json.Marshal(p)
	var fields map[string]any
	_ = json.Unmarshal(raw, &fields)
	return fields
}

func itoa(n int) string {
	raw, _ := json.Marshal(n)
	return string(raw)
}
