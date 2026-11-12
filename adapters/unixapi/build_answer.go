package unixapi

import (
	"github.com/underpass-ai/axlr-bots/application"
)

// buildAnswer is GET /v1/build: the build the control plane runs, the
// one installed on disk, and whether the running one is stale. A client
// that finds no such route talks to a control plane older than it.
type buildAnswer struct {
	Running   buildIdentityDTO `json:"running"`
	Installed buildIdentityDTO `json:"installed"`
	Stale     bool             `json:"stale"`
	Reason    string           `json:"reason,omitempty"`
}

func newBuildAnswer(status application.BuildStatus) buildAnswer {
	return buildAnswer{Running: newBuildIdentityDTO(status.Running), Installed: newBuildIdentityDTO(status.Installed), Stale: status.Stale, Reason: status.Reason}
}
