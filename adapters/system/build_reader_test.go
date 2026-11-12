package system

import (
	"os"
	"path/filepath"
	"runtime/debug"
	"testing"

	"github.com/underpass-ai/axlr-bots/domain"
	release "github.com/underpass-ai/axlr-bots/internal/buildinfo"
)

func TestBuildReaderReadsTheRunningAndTheInstalledBinary(t *testing.T) {
	reader := BuildReader{}
	running := reader.Running()
	installed, err := reader.Installed()
	if err != nil {
		t.Fatal(err)
	}
	// The test binary is the executable: both reads see the same build.
	if running != installed {
		t.Fatalf("running %+v, installed %+v", running, installed)
	}
	if _, err := (BuildReader{Path: filepath.Join(t.TempDir(), "missing")}).Installed(); err == nil {
		t.Fatal("a missing binary cannot be read")
	}
	notGo := filepath.Join(t.TempDir(), "script")
	if err := os.WriteFile(notGo, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := (BuildReader{Path: notGo}).Installed(); err == nil {
		t.Fatal("a file that is no Go binary has no build")
	}
}

func TestIdentityTakesTheVersionControlSettings(t *testing.T) {
	info := &debug.BuildInfo{Main: debug.Module{Version: "v1.2.3"}, Settings: []debug.BuildSetting{
		{Key: "vcs.revision", Value: "abc"}, {Key: "vcs.time", Value: "2026-11-12T12:56:26Z"}, {Key: "vcs.modified", Value: "true"}, {Key: "GOOS", Value: "linux"},
	}}
	want := domain.BuildIdentity{Version: "v1.2.3", Revision: "abc", Time: "2026-11-12T12:56:26Z", Modified: true}
	if got := identity(info); got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestIdentityTakesTheReleaseStamp(t *testing.T) {
	const flags = "-s -w -X github.com/underpass-ai/axlr-bots/internal/buildinfo.Version=0.4.0 -X github.com/underpass-ai/axlr-bots/internal/buildinfo.Revision=def -X github.com/underpass-ai/axlr-bots/internal/buildinfo.Time=2026-11-12T13:00:00Z"
	// A container build: no version control settings, the module (devel).
	stamped := &debug.BuildInfo{Main: debug.Module{Version: "(devel)"}, Settings: []debug.BuildSetting{{Key: "-ldflags", Value: flags}}}
	want := domain.BuildIdentity{Version: "0.4.0", Revision: "def", Time: "2026-11-12T13:00:00Z"}
	if got := identity(stamped); got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	// Go's own commit wins over the stamp's; the stamp's version still names the release.
	both := &debug.BuildInfo{Main: debug.Module{Version: "v0.4.0"}, Settings: []debug.BuildSetting{{Key: "-ldflags", Value: flags}, {Key: "vcs.revision", Value: "abc"}, {Key: "vcs.time", Value: "t"}}}
	want = domain.BuildIdentity{Version: "0.4.0", Revision: "abc", Time: "t"}
	if got := identity(both); got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestRunningTakesThisProcessesStamp(t *testing.T) {
	saved := release.Stamped()
	t.Cleanup(func() { release.Version, release.Revision, release.Time = saved.Version, saved.Revision, saved.Time })
	release.Version, release.Revision, release.Time = "9.9.9", "fed", "t"
	got := BuildReader{}.Running()
	if got.Version != "9.9.9" || got.Revision == "" {
		t.Fatalf("Running = %+v, want the stamped release and a commit", got)
	}
}
