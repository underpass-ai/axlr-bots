package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/underpass-ai/axlr-bots/adapters/pki"
)

// enrollmentRecord is what enroll leaves beside the credentials: the
// node id the certificate names, the labels the control plane gave the
// node and the control plane's URL, so the agent needs no flags.
type enrollmentRecord struct {
	NodeID  string            `json:"nodeId"`
	Labels  map[string]string `json:"labels,omitempty"`
	Control string            `json:"control"`
	Serial  string            `json:"serial"`
}

func recordPath(stateDir string) string {
	return filepath.Join(stateDir, pki.CredentialsDirName, "enrollment.json")
}

func (r enrollmentRecord) save(stateDir string) error {
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(recordPath(stateDir), append(data, '\n'), 0o644)
}

// loadRecord reads the record; a missing one is an empty record.
func loadRecord(stateDir string) (enrollmentRecord, error) {
	data, err := os.ReadFile(recordPath(stateDir))
	if errors.Is(err, os.ErrNotExist) {
		return enrollmentRecord{}, nil
	}
	if err != nil {
		return enrollmentRecord{}, err
	}
	var record enrollmentRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return enrollmentRecord{}, err
	}
	return record, nil
}
