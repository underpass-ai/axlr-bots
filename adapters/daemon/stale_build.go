package daemon

import (
	"errors"
	"log"

	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

// ErrStaleBuild stops the daemon when a newer binary was installed over
// the running one: it exits with an error, so a service manager that
// restarts on failure starts the installed build.
var ErrStaleBuild = errors.New("daemon: the installed binary is a newer build than the running one")

// logBuild says at start which build serves and what it cannot compare.
func logBuild(status application.BuildStatus, logger *log.Logger) {
	if status.Reason != "" {
		logger.Printf("daemon: build %s (installed build not compared: %s)", describeBuild(status.Running), status.Reason)
		return
	}
	logger.Printf("daemon: build %s", describeBuild(status.Running))
}

// describeBuild is a build's commit, short, with its time and changes.
func describeBuild(build domain.BuildIdentity) string {
	if !build.Known() {
		return "without a commit"
	}
	text := build.Revision
	if len(text) > 12 {
		text = text[:12]
	}
	if build.Time != "" {
		text += " (" + build.Time + ")"
	}
	if build.Modified {
		text += " with changes"
	}
	return text
}
