package application

import (
	"context"
	"io"

	"github.com/underpass-ai/axlr-bots/domain"
)

// GetArtifact opens an artifact the control plane keeps for an
// execution: one the execution lists as stored, by name.
type GetArtifact struct {
	deps Dependencies
}

// NewGetArtifact wires the use case.
func NewGetArtifact(deps Dependencies) GetArtifact {
	return GetArtifact{deps: deps}
}

// Execute returns the artifact's listing and its bytes; the caller
// closes them. An artifact that is not listed as stored is not found.
func (uc GetArtifact) Execute(ctx context.Context, id domain.ExecutionID, name string) (domain.Artifact, io.ReadCloser, error) {
	if err := domain.ValidateArtifactName(name); err != nil {
		return domain.Artifact{}, nil, err
	}
	execution, err := uc.deps.Executions.Get(ctx, id)
	if err != nil {
		return domain.Artifact{}, nil, err
	}
	for _, artifact := range execution.Result().Artifacts {
		if artifact.Name != name || !artifact.Stored {
			continue
		}
		content, err := uc.deps.Artifacts.OpenArtifact(ctx, id, name)
		if err != nil {
			return domain.Artifact{}, nil, err
		}
		return artifact, content, nil
	}
	return domain.Artifact{}, nil, domain.NotFoundError{Kind: "artifact", ID: id.String() + "/" + name}
}
