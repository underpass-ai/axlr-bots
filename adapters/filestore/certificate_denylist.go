package filestore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// CertificateDenylist keeps the serials of revoked node certificates in
// <state>/pki/revoked.json, one object per serial with the node and the
// time, rewritten atomically. It is read on every check, so a revocation
// written by the command while the daemon runs is seen at once.
type CertificateDenylist struct {
	path string
	mu   *sync.Mutex
}

// NewCertificateDenylist keeps the denylist under the state directory.
func NewCertificateDenylist(stateDir string) CertificateDenylist {
	return CertificateDenylist{path: filepath.Join(stateDir, "pki", "revoked.json"), mu: &sync.Mutex{}}
}

// Path is the denylist file.
func (d CertificateDenylist) Path() string { return d.path }

// deniedCertificate is one entry of the denylist.
type deniedCertificate struct {
	Serial    string    `json:"serial"`
	NodeID    string    `json:"nodeId"`
	RevokedAt time.Time `json:"revokedAt"`
}

// Deny adds the serial; adding it again changes nothing.
func (d CertificateDenylist) Deny(_ context.Context, serial, node string) error {
	if serial == "" {
		return errors.New("filestore: a serial is required")
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	entries, err := d.read()
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.Serial == serial {
			return nil
		}
	}
	entries = append(entries, deniedCertificate{Serial: serial, NodeID: node, RevokedAt: time.Now().UTC()})
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomically(d.path, append(data, '\n'))
}

// IsDenied reports whether the serial was revoked.
func (d CertificateDenylist) IsDenied(_ context.Context, serial string) (bool, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	entries, err := d.read()
	if err != nil {
		return false, err
	}
	for _, e := range entries {
		if e.Serial == serial {
			return true, nil
		}
	}
	return false, nil
}

func (d CertificateDenylist) read() ([]deniedCertificate, error) {
	data, err := os.ReadFile(d.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var entries []deniedCertificate
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, fmt.Errorf("filestore: %s is not valid JSON: %w", d.path, err)
	}
	return entries, nil
}
