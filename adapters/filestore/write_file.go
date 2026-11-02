// Package filestore keeps aggregates as one JSON file each under a state
// directory, written atomically, so a registered bot survives a restart.
package filestore

import (
	"os"
	"path/filepath"

	"github.com/underpass-ai/axlr-bots/internal/atomicfile"
)

// Modes of the state the store writes: it is private to the daemon's
// user (and its group, for the directories, so an operator in it may
// look), since ids, prompts and answers are in the file names and bodies.
const (
	dirMode  os.FileMode = 0o750
	fileMode os.FileMode = 0o600
)

// writeFile makes the file's directory and writes it atomically.
func writeFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), dirMode); err != nil {
		return err
	}
	return atomicfile.Write(path, data, fileMode)
}
