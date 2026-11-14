// Package topicwire is the wire form of a team's topics on NATS
// (docs/architecture/topics-on-nats.md): the DTOs generated from
// docs/reference/topics.asyncapi.json, the mappers between them and the
// control plane's events and commands, the subjects, the streams and the
// message ids. The domain never sees this package. An application that
// reads a team's topics or publishes on a team imports it for the same
// subjects and envelopes the control plane uses.
package topicwire

//go:generate go run ../../../internal/asyncapigen/cmd/asyncapigen -spec ../../../docs/reference/topics.asyncapi.json -package topicwire -out .

// SpecVersion is the version of the envelope this package writes: the
// spec's TopicEvent.specVersion.
const SpecVersion = "1"
