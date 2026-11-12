package system

import (
	"debug/buildinfo"
	"os"
	"runtime/debug"

	"github.com/underpass-ai/axlr-bots/domain"
	release "github.com/underpass-ai/axlr-bots/internal/buildinfo"
)

// BuildReader reads the build Go recorded in this process and in the
// binary at Path (the executable this process was started from when
// empty: on Linux the path it was installed at, even after a new binary
// replaced it).
type BuildReader struct {
	Path string
}

// Running is this process's build, with the release stamp its own
// variables hold (a build with -trimpath records no -ldflags to read).
func (BuildReader) Running() domain.BuildIdentity {
	var build domain.BuildIdentity
	if info, ok := debug.ReadBuildInfo(); ok {
		build = identity(info)
	}
	return withStamp(build, release.Stamped())
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

// identity is the version control part of a build's information. A
// release stamp (internal/buildinfo, written by the linker) names the
// version, and the commit when Go recorded none: a container build has
// no .git to read it from.
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
	return withStamp(build, release.ReadStamp(info))
}

// withStamp names the release a stamp gives, and its commit when Go
// recorded none.
func withStamp(build domain.BuildIdentity, stamp release.Stamp) domain.BuildIdentity {
	if stamp.Version != "" {
		build.Version = stamp.Version
	}
	if build.Revision == "" && stamp.Revision != "" {
		build.Revision, build.Time = stamp.Revision, stamp.Time
	}
	return build
}
