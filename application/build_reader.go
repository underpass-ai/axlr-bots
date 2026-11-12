package application

import "github.com/underpass-ai/axlr-bots/domain"

// BuildReader tells which build this process runs and which build the
// binary it was started from holds on disk now: they differ once a new
// binary is installed over the one that is running.
type BuildReader interface {
	Running() domain.BuildIdentity
	Installed() (domain.BuildIdentity, error)
}
