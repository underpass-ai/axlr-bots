package topicwire

import (
	"fmt"
	"strings"
)

// The streams, their subjects and the control plane's consumer. The
// control plane defines both streams as it connects.
const (
	// TopicStream keeps a team's topics: TopicPrefix.<tenant>.<team>.<name>.
	TopicStream = "AXLR_TOPIC"
	TopicPrefix = "axlr.topic"
	// PublishStream keeps the publications applications make on a team:
	// PublishPrefix.<tenant>.<team>.<name>.
	PublishStream = "AXLR_PUBLISH"
	PublishPrefix = "axlr.publish"
	// ControlPlaneConsumer is the control plane's durable consumer of
	// PublishStream.
	ControlPlaneConsumer = "control-plane"
	// MsgIDHeader carries the key JetStream drops a repeat by: TopicMsgID
	// out, the publication's idempotency key in.
	MsgIDHeader = "Nats-Msg-Id"
	// MaxKeyLength bounds a publication's idempotency key.
	MaxKeyLength = 128
)

// Token writes a tenant or a team as one subject token: '.', '*', '>'
// and spaces become '_'. Two names that differ only there share a
// token; the envelope keeps them exact.
func Token(raw string) string {
	return strings.NewReplacer(".", "_", "*", "_", ">", "_", " ", "_").Replace(raw)
}

// TopicSubject is where a team's event is published:
// axlr.topic.<tenant>.<team>.<name>, the name's dots kept as tokens.
func TopicSubject(tenant, team, name string) string {
	return TopicPrefix + "." + Token(tenant) + "." + Token(team) + "." + name
}

// TeamTopics is what a reader of the whole team filters on:
// axlr.topic.<tenant>.<team>.>.
func TeamTopics(tenant, team string) string {
	return TopicPrefix + "." + Token(tenant) + "." + Token(team) + ".>"
}

// PublishSubject is where an application publishes on a team:
// axlr.publish.<tenant>.<team>.<name>.
func PublishSubject(tenant, team, name string) string {
	return PublishPrefix + "." + Token(tenant) + "." + Token(team) + "." + name
}

// TopicMsgID is the Nats-Msg-Id of an event's copy for one team: the
// event's id, '@' and the team. One event told to two teams is two
// messages of one stream, and JetStream drops a repeat by this key
// across the whole stream.
func TopicMsgID(id, team string) string { return id + "@" + team }

// Address is what a publication's subject names: the tenant's and the
// team's tokens and the event's name.
type Address struct {
	TenantToken string
	TeamToken   string
	Name        string
}

// ParsePublishSubject reads a publication's subject; an error is a
// subject that is no axlr.publish.<tenant>.<team>.<name>.
func ParsePublishSubject(subject string) (Address, error) {
	rest, ok := strings.CutPrefix(subject, PublishPrefix+".")
	if !ok {
		return Address{}, fmt.Errorf("topics: subject %q is not under %s", subject, PublishPrefix)
	}
	parts := strings.SplitN(rest, ".", 3)
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		return Address{}, fmt.Errorf("topics: subject %q is not %s.<tenant>.<team>.<name>", subject, PublishPrefix)
	}
	return Address{TenantToken: parts[0], TeamToken: parts[1], Name: parts[2]}, nil
}
