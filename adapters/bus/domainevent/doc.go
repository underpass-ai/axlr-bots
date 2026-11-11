// Package domainevent is the wire form of a domain event between teams:
// the DTOs generated from docs/reference/domain-events.asyncapi.json,
// the mappers between them and domain.DomainEvent, and the NATS
// subjects. The domain never sees this package
// (docs/architecture/team-topics.md).
package domainevent

//go:generate go run ../../../internal/asyncapigen/cmd/asyncapigen -spec ../../../docs/reference/domain-events.asyncapi.json -package domainevent -out .

// SpecVersion is the version of the DTO this package writes and reads:
// the spec's DomainEvent.specVersion.
const SpecVersion = "1"
