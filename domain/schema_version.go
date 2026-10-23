package domain

// SchemaVersion names the shape of an event so consumers can tell a
// record written today from one written under a later contract.
type SchemaVersion string

// EventSchemaV1 is the current event schema.
const EventSchemaV1 SchemaVersion = "underpass.ai/events/v1"

func (v SchemaVersion) String() string { return string(v) }
