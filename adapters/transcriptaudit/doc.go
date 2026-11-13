// Package transcriptaudit is the input adapter that audits the whole
// transcript of every execution (docs/architecture/transcripts.md) and
// tells a team about the ones that hide a failure
// (docs/runbooks/transcript-audit.md): the DTOs generated from
// docs/reference/transcript-audit.asyncapi.json, the cutting of a session
// into blocks Jev can judge, the auditor that asks Jev of each block and
// publishes, and its file of verdicts. It is a process beside the world
// (docs/architecture/input-adapters.md): deterministic, it reads and
// publishes through the socket only, as the operator; Jev answers one
// choice per block and nothing else decides.
package transcriptaudit

//go:generate go run ../../internal/asyncapigen/cmd/asyncapigen -spec ../../docs/reference/transcript-audit.asyncapi.json -package transcriptaudit -out .
