//go:build sqlite

// Package sqlitestore keeps the control plane's records that need a
// database: today the whole transcript of each execution's runtime
// session (application.TranscriptStore), in <state>/transcripts.db
// (docs/architecture/transcripts.md). It needs cgo: the driver is
// github.com/mattn/go-sqlite3. Only the control plane links it; the
// node does not.
package sqlitestore
