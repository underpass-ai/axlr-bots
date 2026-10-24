package application

import (
	"context"

	"github.com/underpass-ai/underpass-bots/domain"
)

// Names of the report artifact a completed execution leaves.
const (
	ReportArtifactName = "report.md"
	ReportMediaType    = "text/markdown"
)

// ArtifactStore keeps what an execution hands over, so the task's report
// can be read without the execution record: the runtime's answer as the
// task's report file.
type ArtifactStore interface {
	// SaveReport writes the task's report for the execution and returns
	// the reference (a path) the execution's artifact list carries.
	SaveReport(ctx context.Context, task domain.TaskID, execution domain.ExecutionID, content []byte) (string, error)
}

// ReportReference finds the report artifact of an execution, or "".
func ReportReference(execution domain.Execution) string {
	for _, artifact := range execution.Result().Artifacts {
		if artifact.Name == ReportArtifactName {
			return artifact.Reference
		}
	}
	return ""
}
