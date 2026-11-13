package nodeagent

// SessionReader is a runtime that keeps each session's whole transcript
// in a file the node can read (AXLR: <state>/sessions/<id>.json); the
// node hands it to the control plane before it reports
// (docs/architecture/transcripts.md).
type SessionReader interface {
	ReadSession(id string) ([]byte, error)
}
