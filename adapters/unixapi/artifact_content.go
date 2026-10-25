package unixapi

import (
	"io"

	"github.com/underpass-ai/axlr-bots/domain"
)

// ArtifactContent is a kept artifact: its listing and its bytes, which
// the caller closes.
type ArtifactContent struct {
	Artifact domain.Artifact
	Body     io.ReadCloser
}
