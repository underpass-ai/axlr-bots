package domain

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"slices"
	"strings"
)

// maxOutputFilters bounds each filter list of an output.
const maxOutputFilters = 64

// DefaultOutputEvents are what an output hears when it names no event:
// every move of an incident a person cares about, a reopening among them
// (a resolved incident that fired again, or a proposal sent back: a
// denied, expired or failed apply, a red verify). A repeat is left out:
// it comes every repeat interval of the source. It can be named.
var DefaultOutputEvents = []EventType{
	EventIncidentOpened, EventIncidentReopened, EventIncidentDiagnosed, EventIncidentProposed, EventIncidentApplying,
	EventIncidentVerifying, EventIncidentResolved, EventIncidentEscalated, EventIncidentPostmortemApproved,
}

// outputEventsBeforePostmortem is the default list as a target stored it
// before postmortems: a target that named no event hears the approval
// too, so that list reads as the default.
var outputEventsBeforePostmortem = DefaultOutputEvents[:len(DefaultOutputEvents)-1]

// OutputTarget is one place the control plane announces incidents: a
// signed webhook, a chat's incoming webhook or a local command, with the
// events, tenants and severities it wants. Outputs belong to the control
// plane, not to a bot: one Slack channel hears every brigade.
type OutputTarget struct {
	name       OutputName
	kind       OutputKind
	url        string
	secret     string
	command    []string
	events     []EventType
	tenants    []TenantID
	severities []IncidentSeverity
	panelURL   string
}

// NewOutputTarget validates a target from its definition.
func NewOutputTarget(s OutputTargetSnapshot) (OutputTarget, error) {
	name, err := NewOutputName(s.Name)
	if err != nil {
		return OutputTarget{}, err
	}
	kind, err := ParseOutputKind(s.Type)
	if err != nil {
		return OutputTarget{}, err
	}
	field := "output " + name.String()
	t := OutputTarget{name: name, kind: kind}
	switch kind {
	case OutputWebhook:
		if len(s.Command) > 0 {
			return OutputTarget{}, invalid(field, "a webhook takes a url and a secret, no command")
		}
		if t.url, err = validateNotifyURL(field, s.URL); err != nil {
			return OutputTarget{}, err
		}
		if s.Secret == "" {
			return OutputTarget{}, invalid(field, "a webhook needs a secret to sign its deliveries")
		}
		if t.secret, err = validateCredential(field+" secret", s.Secret); err != nil {
			return OutputTarget{}, err
		}
	case OutputSlack:
		if len(s.Command) > 0 || s.Secret != "" {
			return OutputTarget{}, invalid(field, "a slack target takes its incoming-webhook url alone: the url is the secret")
		}
		if t.url, err = validateNotifyURL(field, s.URL); err != nil {
			return OutputTarget{}, err
		}
	case OutputCommand:
		if s.URL != "" || s.Secret != "" {
			return OutputTarget{}, invalid(field, "a command takes no url or secret")
		}
		if t.command, err = validateCommand(field, s.Command); err != nil {
			return OutputTarget{}, err
		}
	}
	if t.events, err = outputEvents(field, s.Events); err != nil {
		return OutputTarget{}, err
	}
	if t.tenants, err = outputTenants(field, s.Tenants); err != nil {
		return OutputTarget{}, err
	}
	if t.severities, err = outputSeverities(field, s.Severities); err != nil {
		return OutputTarget{}, err
	}
	if s.PanelURL != "" {
		if t.panelURL, err = validateNotifyURL(field+" panel", s.PanelURL); err != nil {
			return OutputTarget{}, err
		}
	}
	return t, nil
}

// ParseOutputEvent reads an incident event by its full name
// (incident.opened) or its short one (opened).
func ParseOutputEvent(raw string) (EventType, error) {
	name := strings.TrimSpace(raw)
	if !strings.HasPrefix(name, "incident.") {
		name = "incident." + name
	}
	event := EventType(name)
	if !event.AboutIncident() {
		return "", invalid("output event", "must be an incident event: opened, repeated, reopened, diagnosed, proposed, applying, verifying, resolved, escalated, postmortem_drafted, postmortem_failed, postmortem_sent_back or postmortem_approved")
	}
	return event, nil
}

func outputEvents(field string, raw []string) ([]EventType, error) {
	if len(raw) == 0 {
		return slices.Clone(DefaultOutputEvents), nil
	}
	if len(raw) > maxOutputFilters {
		return nil, invalid(field+" events", "must be at most 64")
	}
	var events []EventType
	for _, name := range raw {
		event, err := ParseOutputEvent(name)
		if err != nil {
			return nil, err
		}
		if !slices.Contains(events, event) {
			events = append(events, event)
		}
	}
	if slices.Equal(events, outputEventsBeforePostmortem) {
		return slices.Clone(DefaultOutputEvents), nil
	}
	return events, nil
}

func outputTenants(field string, raw []string) ([]TenantID, error) {
	if len(raw) > maxOutputFilters {
		return nil, invalid(field+" tenants", "must be at most 64")
	}
	var tenants []TenantID
	for _, name := range raw {
		tenant, err := NewTenantID(name)
		if err != nil {
			return nil, err
		}
		if !slices.Contains(tenants, tenant) {
			tenants = append(tenants, tenant)
		}
	}
	return tenants, nil
}

func outputSeverities(field string, raw []string) ([]IncidentSeverity, error) {
	if len(raw) > maxOutputFilters {
		return nil, invalid(field+" severities", "must be at most 64")
	}
	var severities []IncidentSeverity
	for _, name := range raw {
		// An empty severity would parse as sev3: a filter names one.
		if strings.TrimSpace(name) == "" {
			return nil, invalid(field+" severities", "must be sev1, sev2, sev3 or sev4")
		}
		severity, err := ParseIncidentSeverity(strings.TrimSpace(name))
		if err != nil {
			return nil, err
		}
		if !slices.Contains(severities, severity) {
			severities = append(severities, severity)
		}
	}
	return severities, nil
}

// Name is the target's name.
func (t OutputTarget) Name() OutputName { return t.name }

// Kind is the target's type.
func (t OutputTarget) Kind() OutputKind { return t.kind }

// URL is where a webhook or a slack target posts; empty for a command.
func (t OutputTarget) URL() string { return t.url }

// Command is the program and its arguments; nil for the others.
func (t OutputTarget) Command() []string { return slices.Clone(t.command) }

// Events are the incident events the target hears.
func (t OutputTarget) Events() []EventType { return slices.Clone(t.events) }

// Tenants are the tenants the target hears; none is every tenant.
func (t OutputTarget) Tenants() []TenantID { return slices.Clone(t.tenants) }

// Severities are the severities the target hears; none is all four.
func (t OutputTarget) Severities() []IncidentSeverity { return slices.Clone(t.severities) }

// PanelURL is the target's own panel base, or empty.
func (t OutputTarget) PanelURL() string { return t.panelURL }

// Wants reports whether the event of the incident reaches the target:
// the event in its list, the incident's tenant and severity in its
// filters when it has them.
func (t OutputTarget) Wants(event EventType, incident Incident) bool {
	if !slices.Contains(t.events, event) {
		return false
	}
	if len(t.tenants) > 0 && !slices.Contains(t.tenants, incident.TenantID()) {
		return false
	}
	return len(t.severities) == 0 || slices.Contains(t.severities, incident.Severity())
}

// PanelLink is the panel page of the incident: <base>/incidents/<id>,
// the base being the target's own panel URL or else fallback (the
// control plane's). Empty when neither is known.
func (t OutputTarget) PanelLink(incident IncidentID, fallback string) string {
	base := t.panelURL
	if base == "" {
		base = fallback
	}
	if base == "" {
		return ""
	}
	return strings.TrimRight(base, "/") + "/incidents/" + url.PathEscape(incident.String())
}

// Host is the host a webhook or a slack target posts to, which says
// where it goes without the path that holds a chat webhook's secret.
// Empty for a command.
func (t OutputTarget) Host() string {
	if t.url == "" {
		return ""
	}
	parsed, err := url.Parse(t.url)
	if err != nil {
		return ""
	}
	return parsed.Host
}

// Fingerprint tells two targets apart without showing what they hold:
// sha256: and the first 12 hex digits of the hash of the type, the URL,
// the secret and the command. Changing any of them changes it.
func (t OutputTarget) Fingerprint() string {
	sum := sha256.Sum256([]byte(t.kind.String() + "\x00" + t.url + "\x00" + t.secret + "\x00" + strings.Join(t.command, "\x00")))
	return "sha256:" + hex.EncodeToString(sum[:6])
}

// Sign is the signature header value of a webhook delivery's body:
// `sha256=` and the HMAC-SHA256 of the body under the target's secret,
// the approval webhook's convention. Empty for the other types.
func (t OutputTarget) Sign(body []byte) string {
	if t.kind != OutputWebhook {
		return ""
	}
	mac := hmac.New(sha256.New, []byte(t.secret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

// Snapshot returns the target as plain data, URL and secret included:
// what the repository stores. Never print it.
func (t OutputTarget) Snapshot() OutputTargetSnapshot {
	s := OutputTargetSnapshot{Name: t.name.String(), Type: t.kind.String(), URL: t.url, Secret: t.secret, Command: t.Command(), PanelURL: t.panelURL}
	for _, event := range t.events {
		s.Events = append(s.Events, event.String())
	}
	for _, tenant := range t.tenants {
		s.Tenants = append(s.Tenants, tenant.String())
	}
	for _, severity := range t.severities {
		s.Severities = append(s.Severities, severity.String())
	}
	return s
}
