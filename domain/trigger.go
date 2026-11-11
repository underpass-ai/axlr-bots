package domain

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"slices"
	"strings"
)

// Limits of a trigger's credentials and of the events it hears.
const (
	minCredentialLength = 16
	maxCredentialLength = 256
	maxTriggerEvents    = 16
)

// Trigger is one way tasks reach the bot, as its definition declares it:
// its type, a name unique within the bot (the type when not given), and
// for a webhook or a timer the objective template each fire renders and
// the credentials or the schedule. An incident trigger names the phase
// of the incident the bot takes (triage, or fix, apply, verify or
// postmortem for a teammate's incident once it reaches that phase) and may narrow the
// bot's level and tools for that phase's tasks (Bot.PolicyFor). A
// question trigger names the teams whose questions the bot answers.
type Trigger struct {
	kind       TriggerType
	name       string
	phase      IncidentPhase
	objective  string
	secret     string
	signingKey string
	schedule   Schedule
	events     []EventPattern
	interrupt  bool
	level      AutonomyLevel
	tools      AllowedTools
	ladder     *Ladder
	severity   IncidentSeverity
	confirm    bool
	topics     []string
	severities []IncidentSeverity
	from       []TeamName
	deliver    DeliveryScope
}

// NewTrigger validates a trigger from its definition.
func NewTrigger(s TriggerSnapshot) (Trigger, error) {
	kind, err := ParseTriggerType(s.Type)
	if err != nil {
		return Trigger{}, err
	}
	name := s.Name
	if name == "" {
		name = kind.String()
	}
	name, err = validateIdentifier("trigger name", name)
	if err != nil {
		return Trigger{}, err
	}
	t := Trigger{kind: kind, name: name}
	if (len(s.Events) > 0 || s.Interrupt) && kind != TriggerEvent {
		return Trigger{}, invalid("trigger "+name, "only an event trigger takes events or interrupt")
	}
	if (s.Phase != "" || s.Level != "" || len(s.Tools) > 0) && kind != TriggerIncident {
		return Trigger{}, invalid("trigger "+name, "only an incident trigger takes a phase, a level or tools")
	}
	if s.Ladder != nil && kind != TriggerIncident {
		return Trigger{}, invalid("trigger "+name, "only an incident trigger of the apply phase takes a ladder")
	}
	if len(s.Severities) > 0 && kind != TriggerIncident {
		return Trigger{}, invalid("trigger "+name, "only an incident trigger of the postmortem phase takes severities")
	}
	if (s.Severity != "" || s.AutoConfirm || len(s.Topics) > 0) && kind != TriggerCloudWatch {
		return Trigger{}, invalid("trigger "+name, "only a cloudwatch trigger takes a severity, autoConfirm or topics")
	}
	if len(s.From) > 0 && kind != TriggerQuestion {
		return Trigger{}, invalid("trigger "+name, "only a question trigger takes from")
	}
	if t.deliver, err = ParseDeliveryScope(s.Deliver); err != nil {
		return Trigger{}, invalid("trigger "+name, err.Error())
	}
	if t.deliver.Team() && kind != TriggerTimer {
		return Trigger{}, invalid("trigger "+name, "only a timer trigger takes deliver: a team's other work comes through its subscriptions")
	}
	t.interrupt = s.Interrupt
	switch kind {
	case TriggerQuestionAnswer:
		return Trigger{}, invalid("trigger "+name, "a bot does not declare question_answer: asking is its consent to the answer")
	case TriggerQuestion:
		if s.Objective != "" || s.Secret != "" || s.SigningKey != "" || s.Every != "" || s.Cron != "" {
			return Trigger{}, invalid("trigger "+name, "a question trigger takes no objective, credentials or schedule")
		}
		if len(s.From) == 0 {
			return Trigger{}, invalid("trigger "+name, "a question trigger names in from the teams it answers")
		}
		if t.from, err = parseTeamList("trigger "+name+" from", s.From); err != nil {
			return Trigger{}, err
		}
		return t, nil
	case TriggerManual, TriggerDelegation:
		if s.Objective != "" || s.Secret != "" || s.SigningKey != "" || s.Every != "" || s.Cron != "" {
			return Trigger{}, invalid("trigger "+name, "a "+kind.String()+" trigger takes no objective, credentials or schedule")
		}
		return t, nil
	case TriggerWebhook, TriggerAlertmanager:
		if s.Every != "" || s.Cron != "" {
			return Trigger{}, invalid("trigger "+name, "a webhook takes no schedule")
		}
		if s.Secret == "" && s.SigningKey == "" {
			return Trigger{}, invalid("trigger "+name, "a webhook needs a secret or a signingKey")
		}
		if t.secret, err = validateCredential("trigger "+name+" secret", s.Secret); err != nil {
			return Trigger{}, err
		}
		if t.signingKey, err = validateCredential("trigger "+name+" signingKey", s.SigningKey); err != nil {
			return Trigger{}, err
		}
		if kind == TriggerAlertmanager && s.Objective == "" {
			return t, nil
		}
	case TriggerCloudWatch:
		if s.Secret != "" || s.SigningKey != "" || s.Every != "" || s.Cron != "" {
			// SNS sets no header of ours and a secret in the URL is a
			// secret in every log: SNS's own signature is the credential.
			return Trigger{}, invalid("trigger "+name, "a cloudwatch trigger takes no secret, signingKey or schedule: SNS's signature authenticates it")
		}
		t.severity, t.confirm = Sev3, s.AutoConfirm
		if s.Severity != "" {
			if t.severity, err = ParseIncidentSeverity(s.Severity); err != nil {
				return Trigger{}, invalid("trigger "+name, err.Error())
			}
		}
		if t.topics, err = validateSNSTopics(s.Topics); err != nil {
			return Trigger{}, invalid("trigger "+name, err.Error())
		}
		if t.confirm && len(t.topics) == 0 {
			// Without a list, confirming by itself would let any AWS
			// account's topic subscribe the route and open incidents.
			return Trigger{}, invalid("trigger "+name, "autoConfirm needs topics: the SNS topic ARNs the trigger takes")
		}
		if s.Objective == "" {
			return t, nil
		}
	case TriggerIncident:
		if s.Secret != "" || s.SigningKey != "" || s.Every != "" || s.Cron != "" {
			return Trigger{}, invalid("trigger "+name, "an incident trigger takes no credentials or schedule")
		}
		if t.phase, err = ParseIncidentPhase(s.Phase); err != nil {
			return Trigger{}, invalid("trigger "+name, err.Error())
		}
		if s.Level != "" {
			if t.level, err = ParseAutonomyLevel(s.Level); err != nil {
				return Trigger{}, invalid("trigger "+name, err.Error())
			}
		}
		if t.tools, err = NewAllowedTools(s.Tools); err != nil {
			return Trigger{}, invalid("trigger "+name, err.Error())
		}
		if s.Ladder != nil {
			if t.phase != IncidentPhaseApply {
				return Trigger{}, invalid("trigger "+name, "only an incident trigger of the apply phase takes a ladder")
			}
			ladder, err := NewLadder(*s.Ladder)
			if err != nil {
				return Trigger{}, invalid("trigger "+name, err.Error())
			}
			t.ladder = &ladder
		}
		if t.severities, err = postmortemSeverities(t.phase, s.Severities); err != nil {
			return Trigger{}, invalid("trigger "+name, err.Error())
		}
		if s.Objective == "" {
			return t, nil
		}
	case TriggerEvent:
		if s.Secret != "" || s.SigningKey != "" || s.Every != "" || s.Cron != "" {
			return Trigger{}, invalid("trigger "+name, "an event trigger takes no credentials or schedule")
		}
		if len(s.Events) == 0 || len(s.Events) > maxTriggerEvents {
			return Trigger{}, invalid("trigger "+name, "an event trigger names 1 to 16 events or patterns")
		}
		for _, raw := range s.Events {
			pattern, err := NewEventPattern(raw)
			if err != nil {
				return Trigger{}, invalid("trigger "+name, err.Error())
			}
			t.events = append(t.events, pattern)
		}
		if s.Objective == "" {
			return t, nil
		}
	case TriggerTimer:
		if s.Secret != "" || s.SigningKey != "" {
			return Trigger{}, invalid("trigger "+name, "a timer takes no credentials")
		}
		if t.schedule, err = NewSchedule(s.Every, s.Cron); err != nil {
			return Trigger{}, invalid("trigger "+name, err.Error())
		}
	}
	if err := ValidateObjectiveTemplate(s.Objective); err != nil {
		return Trigger{}, invalid("trigger "+name+" objective", err.Error())
	}
	t.objective = strings.TrimSpace(s.Objective)
	return t, nil
}

// validateCredential accepts an empty credential or one of 16 to 256
// printable characters without spaces.
func validateCredential(field, raw string) (string, error) {
	if raw == "" {
		return "", nil
	}
	if len(raw) < minCredentialLength || len(raw) > maxCredentialLength {
		return "", invalid(field, "must be 16 to 256 characters")
	}
	for _, r := range raw {
		if r <= 0x20 || r == 0x7f {
			return "", invalid(field, "must not contain spaces or control characters")
		}
	}
	return raw, nil
}

// Type is the trigger's type.
func (t Trigger) Type() TriggerType { return t.kind }

// Name is the trigger's name, unique within the bot.
func (t Trigger) Name() string { return t.name }

// Ref names the trigger on a task.
func (t Trigger) Ref() TriggerRef { return TriggerRef{Type: t.kind, Name: t.name} }

// ObjectiveTemplate is the template each fire renders into the task's
// objective; empty for a manual or delegation trigger, and for an event
// trigger that keeps the default (DefaultEventObjective).
func (t Trigger) ObjectiveTemplate() string { return t.objective }

// Phase is the incident's phase an incident trigger takes, triage by
// default; empty for the other types.
func (t Trigger) Phase() IncidentPhase { return t.phase }

// Level is the level an incident trigger runs its phase's tasks at, and
// false when it keeps the bot's.
func (t Trigger) Level() (AutonomyLevel, bool) { return t.level, t.level != "" }

// Ladder is the apply trigger's progressive autonomy, and false for a
// trigger without one: a bot's explicit opt-in.
func (t Trigger) Ladder() (Ladder, bool) {
	if t.ladder == nil {
		return Ladder{}, false
	}
	return *t.ladder, true
}

// Tools is the allow-list an incident trigger runs its phase's tasks
// with, and false when it keeps the bot's (no list, or an empty one).
func (t Trigger) Tools() (AllowedTools, bool) { return t.tools, t.tools.Len() > 0 }

// Severity is the severity of the incidents a cloudwatch trigger opens;
// zero for the other types.
func (t Trigger) Severity() IncidentSeverity { return t.severity }

// AutoConfirm reports whether a cloudwatch trigger confirms an SNS
// subscription by itself; by default a person does.
func (t Trigger) AutoConfirm() bool { return t.confirm }

// Topics are the SNS topic ARNs a cloudwatch trigger takes messages
// from; none means any topic SNS signed for.
func (t Trigger) Topics() []string { return append([]string(nil), t.topics...) }

// AllowsTopic reports whether a cloudwatch trigger takes a message of
// the topic: any when it lists none.
func (t Trigger) AllowsTopic(arn string) bool {
	return len(t.topics) == 0 || slices.Contains(t.topics, arn)
}

// DefaultPostmortemSeverities are the severities a postmortem is written
// for when the trigger of the postmortem phase does not say (axlr-sre
// design §4).
func DefaultPostmortemSeverities() []IncidentSeverity { return []IncidentSeverity{Sev1, Sev2} }

// TriggerWritesPostmortemByDefault reports a severity a postmortem is
// written for when no trigger says otherwise.
func TriggerWritesPostmortemByDefault(severity IncidentSeverity) bool {
	return slices.Contains(DefaultPostmortemSeverities(), severity)
}

// postmortemSeverities validates what the postmortem phase's trigger
// lists: only that phase takes a list, no repeats, and an empty entry is
// a mistake rather than sev3. An absent list stays absent, so a
// definition reads back as it was written.
func postmortemSeverities(phase IncidentPhase, raw []string) ([]IncidentSeverity, error) {
	if len(raw) > 0 && phase != IncidentPhasePostmortem {
		return nil, invalid("severities", "only the postmortem phase takes them")
	}
	var severities []IncidentSeverity
	for _, value := range raw {
		severity, err := ParseIncidentSeverity(value)
		if err != nil || value == "" {
			return nil, invalid("severities", "must be sev1, sev2, sev3 or sev4")
		}
		if slices.Contains(severities, severity) {
			return nil, invalid("severities", "repeat "+value)
		}
		severities = append(severities, severity)
	}
	return severities, nil
}

// WritesPostmortemFor reports whether the trigger of the postmortem phase
// writes one for an incident of the severity: the severities it lists, or
// DefaultPostmortemSeverities when it lists none.
func (t Trigger) WritesPostmortemFor(severity IncidentSeverity) bool {
	if len(t.severities) == 0 {
		return TriggerWritesPostmortemByDefault(severity)
	}
	return slices.Contains(t.severities, severity)
}

// From lists the teams whose questions a question trigger answers; none
// for the other types.
func (t Trigger) From() []TeamName { return slices.Clone(t.from) }

// AnswersFrom reports whether a question trigger answers the team's
// questions.
func (t Trigger) AnswersFrom(team TeamName) bool {
	return t.kind == TriggerQuestion && slices.Contains(t.from, team)
}

// Schedule is when a timer fires; zero for the other types.
func (t Trigger) Schedule() Schedule { return t.schedule }

// Authenticates reports whether a webhook request may fire the trigger:
// the presented secret equals the trigger's, or the signature header
// (`sha256=<hex>`, or the hex alone) is the HMAC-SHA256 of the body
// under the signing key. Comparisons take constant time.
func (t Trigger) Authenticates(presentedSecret, signature string, body []byte) bool {
	if t.kind != TriggerWebhook && t.kind != TriggerAlertmanager {
		return false
	}
	if t.secret != "" && presentedSecret != "" && subtle.ConstantTimeCompare([]byte(presentedSecret), []byte(t.secret)) == 1 {
		return true
	}
	if t.signingKey != "" && signature != "" {
		mac := hmac.New(sha256.New, []byte(t.signingKey))
		mac.Write(body)
		want := hex.EncodeToString(mac.Sum(nil))
		got := strings.TrimPrefix(strings.TrimSpace(signature), "sha256=")
		if subtle.ConstantTimeCompare([]byte(strings.ToLower(got)), []byte(want)) == 1 {
			return true
		}
	}
	return false
}

// TeamEventTrigger is the trigger a bot hears its team by when it
// declares no event trigger of its own.
func TeamEventTrigger() Trigger { return Trigger{kind: TriggerEvent, name: "team"} }

// Snapshot returns the trigger as plain data, credentials included: what
// the repository stores. Redact it before printing.
// Deliver says whether the bot or one member of its team takes the
// trigger's fires (a timer only).
func (t Trigger) Deliver() DeliveryScope { return t.deliver }

func (t Trigger) Snapshot() TriggerSnapshot {
	s := TriggerSnapshot{Type: t.kind.String(), Name: t.name, Objective: t.objective, Secret: t.secret, SigningKey: t.signingKey}
	if t.kind == TriggerTimer {
		s.Every, s.Cron = t.schedule.Snapshot()
	}
	for _, pattern := range t.events {
		s.Events = append(s.Events, pattern.String())
	}
	s.Interrupt = t.interrupt
	if t.kind == TriggerCloudWatch {
		s.Severity, s.AutoConfirm = t.severity.String(), t.confirm
		if len(t.topics) > 0 {
			s.Topics = append([]string(nil), t.topics...)
		}
	}
	s.Level = t.level.String()
	if t.tools.Len() > 0 {
		s.Tools = t.tools.Names()
	}
	if t.ladder != nil {
		ladder := t.ladder.Snapshot()
		s.Ladder = &ladder
	}
	for _, severity := range t.severities {
		s.Severities = append(s.Severities, severity.String())
	}
	s.From = teamStrings(t.from)
	s.Deliver = t.deliver.String()
	if t.phase != IncidentPhaseTriage {
		// Triage is the default: a definition that did not name it
		// stores as it did before incident triggers had phases.
		s.Phase = t.phase.String()
	}
	return s
}
