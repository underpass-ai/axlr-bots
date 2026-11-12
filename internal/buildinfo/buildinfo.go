// Package buildinfo holds the release a binary was built as. A release
// build stamps it with the linker:
//
//	go build -ldflags "-X github.com/underpass-ai/axlr-bots/internal/buildinfo.Version=0.4.0 \
//	  -X github.com/underpass-ai/axlr-bots/internal/buildinfo.Revision=<commit> \
//	  -X github.com/underpass-ai/axlr-bots/internal/buildinfo.Time=<RFC 3339>"
//
// A build from a container's context carries no version control
// information (no .git), so the stamp is what names it. Version is a
// semantic version without a leading v, the image tag's spelling.
//
// Build without -trimpath: Go then records the -ldflags in the binary,
// and ReadStamp reads the stamp of a binary on disk (the installed one
// the daemon compares itself with). With -trimpath only the running
// process knows its stamp, through Stamped.
package buildinfo

import (
	"runtime/debug"
	"strings"
)

// The stamped values; empty in a build that was not stamped.
var (
	Version  string
	Revision string
	Time     string
)

// Stamp is what the linker's -X wrote into this package in a build.
type Stamp struct {
	Version  string
	Revision string
	Time     string
}

// Stamped is the stamp this process's own variables hold.
func Stamped() Stamp {
	return Stamp{Version: Version, Revision: Revision, Time: Time}
}

// symbol is this package's import path, as -X names its variables.
const symbol = "github.com/underpass-ai/axlr-bots/internal/buildinfo."

// ReadStamp reads the -X values for this package from the -ldflags Go
// recorded in a build: the same for the running process and for a
// binary read from disk, whose variables cannot be reached otherwise.
func ReadStamp(info *debug.BuildInfo) Stamp {
	var stamp Stamp
	if info == nil {
		return stamp
	}
	for _, setting := range info.Settings {
		if setting.Key == "-ldflags" {
			stamp = parseLDFlags(setting.Value)
		}
	}
	return stamp
}

// parseLDFlags picks this package's -X name=value pairs out of a linker
// flag string, in either spelling (-X name=value, -X=name=value).
func parseLDFlags(flags string) Stamp {
	var stamp Stamp
	fields := strings.Fields(flags)
	for i := 0; i < len(fields); i++ {
		var assignment string
		switch {
		case fields[i] == "-X" || fields[i] == "--X":
			if i+1 < len(fields) {
				i++
				assignment = fields[i]
			}
		case strings.HasPrefix(fields[i], "-X="):
			assignment = strings.TrimPrefix(fields[i], "-X=")
		case strings.HasPrefix(fields[i], "--X="):
			assignment = strings.TrimPrefix(fields[i], "--X=")
		}
		name, value, ok := strings.Cut(strings.Trim(assignment, `"'`), "=")
		if !ok || !strings.HasPrefix(name, symbol) {
			continue
		}
		value = strings.Trim(value, `"'`)
		switch strings.TrimPrefix(name, symbol) {
		case "Version":
			stamp.Version = value
		case "Revision":
			stamp.Revision = value
		case "Time":
			stamp.Time = value
		}
	}
	return stamp
}

// Current is this process's release: the stamped Version, else the
// module version Go recorded (go install module@v1.2.3) without its
// leading v; empty for a development build.
func Current() string {
	if Version != "" {
		return Version
	}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	return ModuleVersion(info.Main.Version)
}

// ModuleVersion is a Go module version as a release names it: no
// leading v, nothing for a development build ((devel)).
func ModuleVersion(raw string) string {
	if raw == "" || raw == "(devel)" {
		return ""
	}
	return strings.TrimPrefix(raw, "v")
}
