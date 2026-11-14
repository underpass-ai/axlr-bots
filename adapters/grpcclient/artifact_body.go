package grpcclient

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"

	"github.com/underpass-ai/axlr-bots/domain"
)

// chunkStream is the receiving end of a download stream whose first
// message names the content and the rest are its chunks.
type chunkStream interface {
	// next answers the next chunk, or io.EOF at the end.
	next() ([]byte, error)
}

// artifactBody reads an artifact's chunks as they arrive, checks its
// size and SHA-256 at the end and cancels the stream when closed.
type artifactBody struct {
	stream   chunkStream
	cancel   func()
	artifact domain.Artifact
	address  string
	pending  []byte
	sum      hash.Hash
	read     int64
	done     bool
}

func newArtifactBody(stream chunkStream, cancel func(), artifact domain.Artifact, address string) *artifactBody {
	return &artifactBody{stream: stream, cancel: cancel, artifact: artifact, address: address, sum: sha256.New()}
}

func (b *artifactBody) Read(p []byte) (int, error) {
	for len(b.pending) == 0 {
		if b.done {
			return 0, io.EOF
		}
		chunk, err := b.stream.next()
		if errors.Is(err, io.EOF) {
			b.done = true
			if err := b.verify(); err != nil {
				return 0, err
			}
			return 0, io.EOF
		}
		if err != nil {
			return 0, fromStatus(b.address, err)
		}
		b.sum.Write(chunk)
		b.read += int64(len(chunk))
		b.pending = chunk
	}
	n := copy(p, b.pending)
	b.pending = b.pending[n:]
	return n, nil
}

// verify compares what arrived with what the first message said.
func (b *artifactBody) verify() error {
	if b.artifact.Size != 0 && b.read != b.artifact.Size {
		return fmt.Errorf("artifact %s: %d bytes arrived, the control plane said %d", b.artifact.Name, b.read, b.artifact.Size)
	}
	if b.artifact.SHA256 != "" && hex.EncodeToString(b.sum.Sum(nil)) != b.artifact.SHA256 {
		return fmt.Errorf("artifact %s does not match the SHA-256 the control plane answered", b.artifact.Name)
	}
	return nil
}

func (b *artifactBody) Close() error {
	b.cancel()
	return nil
}
