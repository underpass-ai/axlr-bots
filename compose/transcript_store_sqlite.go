//go:build sqlite

package compose

import (
	"github.com/underpass-ai/axlr-bots/adapters/sqlitestore"
	"github.com/underpass-ai/axlr-bots/application"
)

// TranscriptStore is the transcripts' store of a build with the sqlite
// tag: SQLite through cgo, <state>/transcripts.db
// (docs/architecture/transcripts.md).
func TranscriptStore(stateDir string) application.TranscriptStore {
	return sqlitestore.NewTranscriptStore(stateDir)
}
