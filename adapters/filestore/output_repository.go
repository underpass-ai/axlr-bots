package filestore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/underpass-ai/axlr-bots/domain"
)

// outputsFile is the one file of the output targets, under the state
// directory: written atomically and 0600 like every record, since a
// chat webhook's URL and a webhook's secret are in it.
const outputsFile = "outputs.json"

// outputsDocument is the file's shape.
type outputsDocument struct {
	Outputs []domain.OutputTargetSnapshot `json:"outputs"`
}

// OutputRepository keeps the control plane's output targets in
// <state>/outputs.json. Its writes are serialized within the process;
// the state lock keeps a second process out.
type OutputRepository struct {
	path string
	mu   *sync.Mutex
}

// NewOutputRepository stores the targets under the state directory.
func NewOutputRepository(stateDir string) OutputRepository {
	return OutputRepository{path: filepath.Join(stateDir, outputsFile), mu: &sync.Mutex{}}
}

// List reads every target, in name order.
func (r OutputRepository) List(_ context.Context) ([]domain.OutputTarget, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.read()
}

// Get reads one target.
func (r OutputRepository) Get(ctx context.Context, name domain.OutputName) (domain.OutputTarget, error) {
	targets, err := r.List(ctx)
	if err != nil {
		return domain.OutputTarget{}, err
	}
	for _, target := range targets {
		if target.Name() == name {
			return target, nil
		}
	}
	return domain.OutputTarget{}, domain.NotFoundError{Kind: "output", ID: name.String()}
}

// Add keeps a new target.
func (r OutputRepository) Add(_ context.Context, target domain.OutputTarget) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	targets, err := r.read()
	if err != nil {
		return err
	}
	for _, existing := range targets {
		if existing.Name() == target.Name() {
			return domain.ConflictError{Kind: "output", ID: target.Name().String()}
		}
	}
	return r.write(append(targets, target))
}

// Remove drops a target.
func (r OutputRepository) Remove(_ context.Context, name domain.OutputName) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	targets, err := r.read()
	if err != nil {
		return err
	}
	kept := slices.DeleteFunc(targets, func(target domain.OutputTarget) bool { return target.Name() == name })
	if len(kept) == len(targets) {
		return domain.NotFoundError{Kind: "output", ID: name.String()}
	}
	return r.write(kept)
}

// read loads the file; none is no target. A target the domain refuses
// fails the read loudly: a file edited by hand must be fixed, not half
// used.
func (r OutputRepository) read() ([]domain.OutputTarget, error) {
	data, err := os.ReadFile(r.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var document outputsDocument
	if err := json.Unmarshal(data, &document); err != nil {
		return nil, fmt.Errorf("filestore: %s is not valid JSON: %w", outputsFile, err)
	}
	targets := make([]domain.OutputTarget, 0, len(document.Outputs))
	for _, snapshot := range document.Outputs {
		target, err := domain.NewOutputTarget(snapshot)
		if err != nil {
			return nil, fmt.Errorf("filestore: %s: %w", outputsFile, err)
		}
		targets = append(targets, target)
	}
	slices.SortFunc(targets, func(a, b domain.OutputTarget) int { return strings.Compare(a.Name().String(), b.Name().String()) })
	return targets, nil
}

func (r OutputRepository) write(targets []domain.OutputTarget) error {
	document := outputsDocument{Outputs: make([]domain.OutputTargetSnapshot, 0, len(targets))}
	for _, target := range targets {
		document.Outputs = append(document.Outputs, target.Snapshot())
	}
	data, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return err
	}
	return writeFile(r.path, append(data, '\n'))
}
