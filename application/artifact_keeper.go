package application

import (
	"context"
	"io"
	"strconv"
	"strings"

	"github.com/underpass-ai/axlr-bots/domain"
)

// Bounds of what the control plane keeps for one execution, beside
// domain.MaxArtifactBytes per artifact.
const (
	MaxArtifactBytesPerExecution = 256 << 20
	// DefaultArtifactMediaType is the media type of an artifact listed
	// without one.
	DefaultArtifactMediaType = "application/octet-stream"
)

// artifactKeeper copies the artifacts an execution delivered into the
// store, each checked against its size and hash, and marks in the
// execution's list the ones it keeps. A node uploads its executions'
// artifacts before it reports (UploadArtifact); for an execution the
// control plane ran itself, the keeper reads them from its own
// workspace when the run is settled.
type artifactKeeper struct {
	deps Dependencies
	rec  recorder
}

// keep stores one artifact after checking it may be kept: the bounds per
// artifact and per execution. A later artifact of the same name
// replaces an earlier one, as a later delivery does in AXLR.
func (k artifactKeeper) keep(ctx context.Context, execution domain.ExecutionID, artifact domain.Artifact, content io.Reader) error {
	if err := domain.ValidateArtifactName(artifact.Name); err != nil {
		return err
	}
	if !artifact.Verifiable() {
		return domain.ValidationError{Field: "artifact " + artifact.Name, Reason: "needs a size of at most 32 MiB and a SHA-256"}
	}
	if artifact.MediaType == "" {
		artifact.MediaType = DefaultArtifactMediaType
	}
	if err := domain.ValidateMediaType(artifact.MediaType); err != nil {
		return err
	}
	stored, err := k.deps.Artifacts.StoredArtifacts(ctx, execution)
	if err != nil {
		return err
	}
	count, total := 0, artifact.Size
	for _, kept := range stored {
		if kept.Name == artifact.Name {
			continue
		}
		count++
		total += kept.Size
	}
	if count >= domain.MaxArtifactsPerExecution {
		return domain.ValidationError{Field: "artifacts", Reason: "an execution keeps at most 64"}
	}
	if total > MaxArtifactBytesPerExecution {
		return domain.ValidationError{Field: "artifacts", Reason: "an execution keeps at most 256 MiB"}
	}
	listing := domain.Artifact{Name: artifact.Name, MediaType: artifact.MediaType, Size: artifact.Size, SHA256: artifact.SHA256, Reference: artifact.Reference, Stored: true}
	return k.deps.Artifacts.SaveArtifact(ctx, execution, listing, newVerifiedReader(content, artifact))
}

// collect reads from the control plane's workspace the artifacts of an
// execution it ran itself. A file that cannot be read or does not match
// its listing is audited and left out: the run's outcome stands.
func (k artifactKeeper) collect(ctx context.Context, bot domain.Bot, execution domain.Execution, artifacts []domain.Artifact) {
	for _, artifact := range artifacts {
		if !artifact.Verifiable() {
			continue
		}
		err := k.copyFromWorkspace(ctx, bot, execution, artifact)
		if err != nil {
			_ = k.rec.audit(ctx, "artifact.store", execution.ID().String(), domain.AuditFailed, map[string]string{"artifact": artifact.Name, "reason": err.Error()})
			continue
		}
		_ = k.rec.audit(ctx, "artifact.store", execution.ID().String(), domain.AuditOK, map[string]string{"artifact": artifact.Name, "size": strconv.FormatInt(artifact.Size, 10), "sha256": artifact.SHA256})
	}
}

func (k artifactKeeper) copyFromWorkspace(ctx context.Context, bot domain.Bot, execution domain.Execution, artifact domain.Artifact) error {
	file, err := k.deps.Workspace.OpenDelivered(bot.ID(), artifact.Reference)
	if err != nil {
		return err
	}
	defer file.Close()
	return k.keep(ctx, execution.ID(), artifact, file)
}

// mark sets Stored on the artifacts the store keeps with the same name,
// size and hash, whatever the report claimed: the store is the record.
func (k artifactKeeper) mark(ctx context.Context, execution domain.ExecutionID, artifacts []domain.Artifact) []domain.Artifact {
	if len(artifacts) == 0 {
		return artifacts
	}
	stored, err := k.deps.Artifacts.StoredArtifacts(ctx, execution)
	if err != nil {
		stored = nil
	}
	kept := map[string]domain.Artifact{}
	for _, a := range stored {
		kept[a.Name] = a
	}
	out := make([]domain.Artifact, len(artifacts))
	for i, artifact := range artifacts {
		artifact.Stored = false
		if a, ok := kept[artifact.Name]; ok && artifact.Verifiable() && a.Size == artifact.Size && strings.EqualFold(a.SHA256, artifact.SHA256) {
			artifact.Stored = true
		}
		out[i] = artifact
	}
	return out
}
