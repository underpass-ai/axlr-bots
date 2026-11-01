package domain

import "strings"

// TopicKind is the first segment of an event's name: it fixes how much
// the event matters and who may publish it.
type TopicKind string

const (
	// KindError is a task that failed; only the control plane says it.
	KindError TopicKind = "error"
	// KindDanger is something that may hurt; critical.
	KindDanger TopicKind = "danger"
	// KindStop tells the teammates that hear it to stop; critical.
	KindStop TopicKind = "stop"
	// KindWarn is something people and teammates should see now.
	KindWarn TopicKind = "warn"
	// KindDone is a task that finished; only the control plane says it.
	KindDone TopicKind = "done"
	// KindInfo is a fact for the record and whoever subscribed; any name
	// whose first segment is no kind is info.
	KindInfo TopicKind = "info"
)

// Publisher is who puts an event on a team's bus.
type Publisher string

const (
	// PublisherBot is a bot's run, through the emit tool.
	PublisherBot Publisher = "bot"
	// PublisherOperator is a person, through the operator API.
	PublisherOperator Publisher = "operator"
	// PublisherControlPlane is the control plane telling a fact of its own.
	PublisherControlPlane Publisher = "control-plane"
)

// KindOf reads the kind of an event's name: its first segment when that
// is a kind, and info for anything else.
func KindOf(name EventName) TopicKind {
	first, _, _ := strings.Cut(name.String(), ".")
	switch kind := TopicKind(first); kind {
	case KindError, KindDanger, KindStop, KindWarn, KindDone, KindInfo:
		return kind
	}
	return KindInfo
}

// Severity is what the kind fixes: error, danger and stop are critical,
// warn a warning, done and info info.
func (k TopicKind) Severity() Severity {
	switch k {
	case KindError, KindDanger, KindStop:
		return SeverityCritical
	case KindWarn:
		return SeverityWarning
	}
	return SeverityInfo
}

// PublishedBy reports whether the publisher may put an event of the kind
// on the bus: error and done are the control plane's only; the control
// plane says nothing but error, warn and done.
func (k TopicKind) PublishedBy(p Publisher) bool {
	switch k {
	case KindError, KindDone:
		return p == PublisherControlPlane
	case KindWarn:
		return true
	}
	return p != PublisherControlPlane
}

func (k TopicKind) String() string { return string(k) }
