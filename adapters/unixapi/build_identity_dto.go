package unixapi

import "github.com/underpass-ai/axlr-bots/domain"

// buildIdentityDTO is one build on the wire.
type buildIdentityDTO struct {
	Version  string `json:"version,omitempty"`
	Revision string `json:"revision,omitempty"`
	Time     string `json:"time,omitempty"`
	Modified bool   `json:"modified"`
}

func newBuildIdentityDTO(build domain.BuildIdentity) buildIdentityDTO {
	return buildIdentityDTO{Version: build.Version, Revision: build.Revision, Time: build.Time, Modified: build.Modified}
}
