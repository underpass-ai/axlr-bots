package filestore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

// ArtifactStore keeps each task's report under
// <state>/artifacts/<task-id>/report.md, written atomically (a task that
// completes again, after an approval, replaces it), and each execution's
// artifacts under <state>/artifacts/executions/<execution-id>/<name>
// with their listing in .listing/<name>.json beside them: an artifact
// name never starts with a dot, so neither the listing nor a temporary
// file can take an artifact's place.
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

// executionDir is where the execution's artifacts are; an id that is not
// a plain file name is refused.
func (s ArtifactStore) executionDir(execution domain.ExecutionID) (string, error) {
	id := execution.String()
	if id == "" || strings.ContainsAny(id, `/\`) || strings.Contains(id, "..") {
		return "", fmt.Errorf("filestore: execution id %q is not a file name", id)
	}
	return filepath.Join(s.dir, "executions", id), nil
}

// artifactPaths are the bytes and the listing of an artifact.
func (s ArtifactStore) artifactPaths(execution domain.ExecutionID, name string) (string, string, error) {
	if err := domain.ValidateArtifactName(name); err != nil {
		return "", "", err
	}
	// Checked here too, beside the join, where a reader of this file
	// (and a path analysis) sees that the name cannot leave the directory.
	if strings.Contains(name, "..") || strings.ContainsAny(name, `/\`) {
		return "", "", fmt.Errorf("filestore: artifact name %q is not a file name", name)
	}
	dir, err := s.executionDir(execution)
	if err != nil {
		return "", "", err
	}
	return filepath.Join(dir, name), filepath.Join(dir, ".listing", name+".json"), nil
}

// SaveArtifact streams content to a temporary file, renames it over the
// artifact once all of it was read and fsynced, then writes its listing.
// A read that fails (the checking reader refused the bytes) keeps nothing.
func (s ArtifactStore) SaveArtifact(_ context.Context, execution domain.ExecutionID, artifact domain.Artifact, content io.Reader) error {
	path, listing, err := s.artifactPaths(execution, artifact.Name)
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "."+artifact.Name+".*.tmp")
	if err != nil {
		return err
	}
	discard := func(cause error) error {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
		return cause
	}
	if _, err := io.Copy(tmp, content); err != nil {
		return discard(err)
	}
	if err := tmp.Sync(); err != nil {
		return discard(err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		_ = os.Remove(tmp.Name())
		return fmt.Errorf("filestore: rename %s: %w", path, err)
	}
	data, err := json.MarshalIndent(artifact, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomically(listing, append(data, '\n'))
}

// StoredArtifacts reads the listings of the execution's artifacts, in
// name order; a listing without its bytes is left out.
func (s ArtifactStore) StoredArtifacts(_ context.Context, execution domain.ExecutionID) ([]domain.Artifact, error) {
	dir, err := s.executionDir(execution)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(filepath.Join(dir, ".listing"))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var artifacts []domain.Artifact
	for _, entry := range entries {
		name, ok := strings.CutSuffix(entry.Name(), ".json")
		if entry.IsDir() || !ok || domain.ValidateArtifactName(name) != nil {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, ".listing", entry.Name()))
		if err != nil {
			return nil, err
		}
		var artifact domain.Artifact
		if err := json.Unmarshal(data, &artifact); err != nil {
			return nil, fmt.Errorf("filestore: artifact listing %s of %s is not valid JSON: %w", name, execution, err)
		}
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			continue
		}
		artifacts = append(artifacts, artifact)
	}
	return artifacts, nil
}

// OpenArtifact opens the artifact's bytes.
func (s ArtifactStore) OpenArtifact(_ context.Context, execution domain.ExecutionID, name string) (io.ReadCloser, error) {
	path, _, err := s.artifactPaths(execution, name)
	if err != nil {
		return nil, err
	}
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, domain.NotFoundError{Kind: "artifact", ID: execution.String() + "/" + name}
	}
	return file, err
}
