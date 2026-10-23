package main

import (
	"bytes"
	"io"
)

// bytesReader wraps raw bytes for a JSON decoder.
func bytesReader(raw []byte) io.Reader { return bytes.NewReader(raw) }
