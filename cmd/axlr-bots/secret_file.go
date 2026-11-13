package main

import (
	"errors"
	"io"
	"os"
	"strings"
)

// stdin is what --secret-file - reads; a test replaces it.
var stdin io.Reader = os.Stdin

// maxSecretFileBytes bounds a secret read from a file or stdin.
const maxSecretFileBytes = 8 << 10

// readSecretFile reads a secret from a file or stdin (-), without its
// trailing newline.
func readSecretFile(path string) (string, error) {
	var reader io.Reader
	if path == "-" {
		reader = stdin
	} else {
		f, err := os.Open(path)
		if err != nil {
			return "", err
		}
		defer f.Close()
		reader = f
	}
	raw, err := io.ReadAll(io.LimitReader(reader, maxSecretFileBytes+1))
	if err != nil {
		return "", err
	}
	if len(raw) > maxSecretFileBytes {
		return "", errors.New(path + ": more than 8 KiB")
	}
	return strings.TrimSpace(string(raw)), nil
}
