package application

import (
	"context"
	"io"
	"strconv"

	"github.com/underpass-ai/axlr-bots/domain"
)

// UploadArtifact takes the bytes of an artifact a node's execution
// delivered: only from the node the execution is assigned to and while
// it runs (a node uploads before it reports), within the bounds per
// artifact and per execution, and kept only when the bytes are the size
// and the SHA-256 the upload names. The report then lists the artifact
// as stored. Repeating an upload replaces it.
type UploadArtifact struct {
	deps   Dependencies
	rec    recorder
	keeper artifactKeeper
}

// NewUploadArtifact wires the use case.
func NewUploadArtifact(deps Dependencies) UploadArtifact {
	rec := recorder{deps: deps}
	return UploadArtifact{deps: deps, rec: rec, keeper: artifactKeeper{deps: deps, rec: rec}}
}

// Execute stores the artifact and returns its listing.
func (uc UploadArtifact) Execute(ctx context.Context, node domain.NodeID, id domain.ExecutionID, artifact domain.Artifact, content io.Reader) (domain.Artifact, error) {
	execution, err := uc.deps.Executions.Get(ctx, id)
	if err != nil {
		return domain.Artifact{}, err
	}
	if execution.NodeID() != node {
		return domain.Artifact{}, uc.refuse(ctx, id, artifact, domain.StateError{Subject: "execution " + id.String(), Current: "assigned to " + orNobody(execution.NodeID()), Attempted: "take an artifact from node " + node.String()})
	}
	if execution.Outcome() != domain.ExecutionRunning {
		return domain.Artifact{}, uc.refuse(ctx, id, artifact, domain.StateError{Subject: "execution " + id.String(), Current: execution.Outcome().String(), Attempted: "take an artifact"})
	}
	if artifact.MediaType == "" {
		artifact.MediaType = DefaultArtifactMediaType
	}
	if err := uc.keeper.keep(ctx, id, artifact, content); err != nil {
		return domain.Artifact{}, uc.refuse(ctx, id, artifact, err)
	}
	details := map[string]string{"artifact": artifact.Name, "node": node.String(), "size": strconv.FormatInt(artifact.Size, 10), "sha256": artifact.SHA256}
	if err := uc.rec.audit(ctx, "artifact.upload", id.String(), domain.AuditOK, details); err != nil {
		return domain.Artifact{}, err
	}
	artifact.Stored = true
	return artifact, nil
}

func (uc UploadArtifact) refuse(ctx context.Context, id domain.ExecutionID, artifact domain.Artifact, cause error) error {
	_ = uc.rec.audit(ctx, "artifact.upload", id.String(), domain.AuditDenied, map[string]string{"artifact": artifact.Name, "reason": cause.Error()})
	return cause
}
