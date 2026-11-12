package application

import "github.com/underpass-ai/axlr-bots/domain"

// BuildStatus compares the running build with the installed one. Stale
// says both name their commit and the installed binary is another
// build: the process runs code that is no longer installed. Reason says
// why nothing could be compared, when that is the case.
type BuildStatus struct {
	Running   domain.BuildIdentity
	Installed domain.BuildIdentity
	Stale     bool
	Reason    string
}

// CheckBuild reads both builds; without a reader there is nothing to say.
func CheckBuild(reader BuildReader) BuildStatus {
	if reader == nil {
		return BuildStatus{Reason: "no build reader"}
	}
	status := BuildStatus{Running: reader.Running()}
	installed, err := reader.Installed()
	if err != nil {
		status.Reason = "the installed binary cannot be read: " + err.Error()
		return status
	}
	status.Installed = installed
	switch {
	case !status.Running.Known():
		status.Reason = "the running build names no commit"
	case !installed.Known():
		status.Reason = "the installed binary names no commit"
	default:
		status.Stale = !status.Running.Matches(installed)
	}
	return status
}
