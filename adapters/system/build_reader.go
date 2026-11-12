package system

import (
	"debug/buildinfo"
	"os"
	"runtime/debug"

	"github.com/underpass-ai/axlr-bots/domain"
)

// BuildReader reads the build Go recorded in this process and in the
// binary at Path (the executable this process was started from when
// empty: on Linux the path it was installed at, even after a new binary
// replaced it).
type BuildReader struct {
	Path string
}

// Running is this process's build.
func (BuildReader) Running() domain.BuildIdentity {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return domain.BuildIdentity{}
	}
	return identity(info)
}

// Installed is the build of the binary on disk now.
func (r BuildReader) Installed() (domain.BuildIdentity, error) {
	path := r.Path
	if path == "" {
		var err error
		if path, err = os.Executable(); err != nil {
			return domain.BuildIdentity{}, err
		}
	}
	info, err := buildinfo.ReadFile(path)
	if err != nil {
		return domain.BuildIdentity{}, err
	}
	return identity(info), nil
}

// identity is the version control part of a build's information.
func identity(info *debug.BuildInfo) domain.BuildIdentity {
	build := domain.BuildIdentity{Version: info.Main.Version}
	for _, setting := range info.Settings {
		switch setting.Key {
		case "vcs.revision":
			build.Revision = setting.Value
		case "vcs.time":
			build.Time = setting.Value
		case "vcs.modified":
			build.Modified = setting.Value == "true"
		}
	}
	return build
}
