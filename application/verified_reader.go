package application

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"

	"github.com/underpass-ai/axlr-bots/domain"
)

// verifiedReader passes an artifact's bytes through and fails as soon as
// they run past the size its listing gives, or at their end when they
// are not that size or do not hash to its SHA-256: the store keeps
// nothing of a read that failed.
type verifiedReader struct {
	source io.Reader
	want   domain.Artifact
	hash   hash.Hash
	read   int64
}

func newVerifiedReader(source io.Reader, want domain.Artifact) *verifiedReader {
	return &verifiedReader{source: io.LimitReader(source, want.Size+1), want: want, hash: sha256.New()}
}

func (v *verifiedReader) Read(p []byte) (int, error) {
	n, err := v.source.Read(p)
	v.hash.Write(p[:n])
	v.read += int64(n)
	field := "artifact " + v.want.Name
	if v.read > v.want.Size {
		return n, domain.ValidationError{Field: field, Reason: fmt.Sprintf("is larger than the %d bytes it lists", v.want.Size)}
	}
	if errors.Is(err, io.ErrUnexpectedEOF) {
		return n, domain.ValidationError{Field: field, Reason: fmt.Sprintf("ended after %d of the %d bytes it lists", v.read, v.want.Size)}
	}
	if err == io.EOF {
		if v.read != v.want.Size {
			return n, domain.ValidationError{Field: field, Reason: fmt.Sprintf("is %d bytes, it lists %d", v.read, v.want.Size)}
		}
		if got := hex.EncodeToString(v.hash.Sum(nil)); got != v.want.SHA256 {
			return n, domain.ValidationError{Field: field, Reason: "does not match its SHA-256 (" + got + ")"}
		}
	}
	return n, err
}
