package main

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// apiTokenFileName is the operator bearer token under the state
// directory, written once with mode 0600.
const apiTokenFileName = "api.token"

// loadOrCreateAPIToken reads the bearer token of the operator address,
// or generates one (256 bits, hex) when the file does not exist. The
// token is in the file and nowhere else: not in the log, not in a unit.
func loadOrCreateAPIToken(path string) (token string, created bool, err error) {
	raw, err := os.ReadFile(path)
	if err == nil {
		token = strings.TrimSpace(string(raw))
		if token == "" {
			return "", false, fmt.Errorf("%s is empty", path)
		}
		return token, false, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return "", false, err
	}
	var random [32]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", false, err
	}
	token = hex.EncodeToString(random[:])
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", false, err
	}
	if err := os.WriteFile(path, []byte(token+"\n"), 0o600); err != nil {
		return "", false, err
	}
	return token, true, nil
}
