package filestore

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

// ArtifactStore keeps each task's report under
// <state>/artifacts/<task-id>/report.md, written atomically. A task that
// completes again (after an approval) replaces its report.
type ArtifactStore struct {
	dir string
}

// NewArtifactStore stores artifacts under the state directory.
func NewArtifactStore(stateDir string) ArtifactStore {
	return ArtifactStore{dir: filepath.Join(stateDir, "artifacts")}
}

func (s ArtifactStore) reportPath(task domain.TaskID) string {
	return filepath.Join(s.dir, task.String(), application.ReportArtifactName)
}

// SaveReport writes the report and returns its path.
func (s ArtifactStore) SaveReport(_ context.Context, task domain.TaskID, _ domain.ExecutionID, content []byte) (string, error) {
	path := s.reportPath(task)
	if err := writeAtomically(path, content); err != nil {
		return "", err
	}
	return path, nil
}

// LoadReport reads the task's report.
func (s ArtifactStore) LoadReport(_ context.Context, task domain.TaskID) ([]byte, error) {
	content, err := os.ReadFile(s.reportPath(task))
	if errors.Is(err, os.ErrNotExist) {
		return nil, domain.NotFoundError{Kind: "report", ID: task.String()}
	}
	return content, err
}
