package application

import (
	"context"
	"io"

	"github.com/underpass-ai/axlr-bots/domain"
)

// Names of the report artifact a completed execution leaves.
const (
	ReportArtifactName = "report.md"
	ReportMediaType    = "text/markdown"
)

// ArtifactStore keeps what an execution hands over, so it can be read
// without the host that ran it: the runtime's answer as the task's
// report file, and the bytes of each artifact the runtime delivered,
// per execution and by name.
type ArtifactStore interface {
	// SaveReport writes the task's report for the execution and returns
	// the reference (a path) the execution's artifact list carries.
	SaveReport(ctx context.Context, task domain.TaskID, execution domain.ExecutionID, content []byte) (string, error)
	// LoadReport reads the task's report; a domain.NotFoundError when the
	// task has none.
	LoadReport(ctx context.Context, task domain.TaskID) ([]byte, error)
	// SaveArtifact writes what content yields as the execution's
	// artifact, replacing one of the same name, and keeps its listing
	// (name, media type, size, hash) beside it. Nothing is kept when
	// reading content fails: the caller's reader checks the bytes.
	SaveArtifact(ctx context.Context, execution domain.ExecutionID, artifact domain.Artifact, content io.Reader) error
	// StoredArtifacts lists what was kept for the execution, by name.
	StoredArtifacts(ctx context.Context, execution domain.ExecutionID) ([]domain.Artifact, error)
	// OpenArtifact reads a kept artifact; a domain.NotFoundError when
	// there is none of that name.
	OpenArtifact(ctx context.Context, execution domain.ExecutionID, name string) (io.ReadCloser, error)
}

// ReportReference finds the report artifact of an execution, or "": the
// one the control plane wrote from the answer, not a delivery the
// runtime happened to name report.md too.
func ReportReference(execution domain.Execution) string {
	for _, artifact := range execution.Result().Artifacts {
		if artifact.Name != ReportArtifactName {
			continue
		}
		if artifact.Source == domain.ArtifactSourceAnswer || (artifact.Source == "" && artifact.SHA256 == "") {
			return artifact.Reference
		}
	}
	return ""
}
