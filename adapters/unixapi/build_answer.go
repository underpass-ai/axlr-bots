package unixapi

import (
	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

// buildAnswer is GET /v1/build: the build the control plane runs, the
// one installed on disk, and whether the running one is stale. A client
// that finds no such route talks to a control plane older than it.
// MinimumAXLR is the least AXLR the operator wants on the nodes (serve
// --min-axlr-version), absent when none: a client flags a node whose
// axlrVersion is older, as it flags one whose nodeVersion is older than
// running.version.
type buildAnswer struct {
	Running     buildIdentityDTO `json:"running"`
	Installed   buildIdentityDTO `json:"installed"`
	Stale       bool             `json:"stale"`
	Reason      string           `json:"reason,omitempty"`
	MinimumAXLR string           `json:"minimumAxlr,omitempty"`
}

func newBuildAnswer(status application.BuildStatus, minimumAXLR domain.SemanticVersion) buildAnswer {
	return buildAnswer{Running: newBuildIdentityDTO(status.Running), Installed: newBuildIdentityDTO(status.Installed), Stale: status.Stale, Reason: status.Reason, MinimumAXLR: minimumAXLR.String()}
}
