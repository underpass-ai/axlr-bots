//go:build !sqlite

package compose

import (
	"github.com/underpass-ai/axlr-bots/adapters/filestore"
	"github.com/underpass-ai/axlr-bots/application"
)

// TranscriptStore is the transcripts' store of a build without the
// sqlite tag: plain files under <state>/transcripts, no cgo
// (docs/architecture/transcripts.md).
func TranscriptStore(stateDir string) application.TranscriptStore {
	return filestore.NewTranscriptStore(stateDir)
}
