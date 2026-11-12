package system

import (
	"os"
	"path/filepath"
	"runtime/debug"
	"testing"

	"github.com/underpass-ai/axlr-bots/domain"
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
