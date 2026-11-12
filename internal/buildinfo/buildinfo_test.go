package buildinfo

import (
	"runtime/debug"
	"testing"
)

func TestReadStampFindsThisPackagesValues(t *testing.T) {
	info := &debug.BuildInfo{Settings: []debug.BuildSetting{
		{Key: "-trimpath", Value: "true"},
		{Key: "-ldflags", Value: `-s -w -X github.com/underpass-ai/axlr-bots/internal/buildinfo.Version=0.4.0 -X=github.com/underpass-ai/axlr-bots/internal/buildinfo.Revision=abc123 -X "github.com/underpass-ai/axlr-bots/internal/buildinfo.Time=2026-11-12T10:00:00Z" -X main.other=x`},
	}}
	got := ReadStamp(info)
	want := Stamp{Version: "0.4.0", Revision: "abc123", Time: "2026-11-12T10:00:00Z"}
	if got != want {
		t.Fatalf("ReadStamp = %+v, want %+v", got, want)
	}
}

func TestReadStampWithoutAStamp(t *testing.T) {
	cases := []*debug.BuildInfo{
		nil,
		{},
		{Settings: []debug.BuildSetting{{Key: "-ldflags", Value: "-s -w"}}},
		{Settings: []debug.BuildSetting{{Key: "-ldflags", Value: "-X"}}},
		{Settings: []debug.BuildSetting{{Key: "-ldflags", Value: "-X github.com/underpass-ai/axlr-bots/internal/buildinfo.Unknown=1 -X noequals"}}},
	}
	for _, info := range cases {
		if got := ReadStamp(info); got != (Stamp{}) {
			t.Fatalf("ReadStamp(%+v) = %+v, want nothing", info, got)
		}
	}
}

func TestParseLDFlagsDoubleDashSpellings(t *testing.T) {
	got := parseLDFlags("--X github.com/underpass-ai/axlr-bots/internal/buildinfo.Version=1.0.0 --X=github.com/underpass-ai/axlr-bots/internal/buildinfo.Time=t")
	if got.Version != "1.0.0" || got.Time != "t" {
		t.Fatalf("parseLDFlags = %+v", got)
	}
}

func TestCurrentPrefersTheStamp(t *testing.T) {
	saved := Version
	t.Cleanup(func() { Version = saved })
	Version = "2.0.0-rc.1"
	if got := Current(); got != "2.0.0-rc.1" {
		t.Fatalf("Current = %q", got)
	}
	Version = ""
	// A test binary records no module version.
	if got := Current(); got != "" {
		t.Fatalf("Current without a stamp = %q, want empty", got)
	}
}

func TestModuleVersion(t *testing.T) {
	for raw, want := range map[string]string{"": "", "(devel)": "", "v0.4.0": "0.4.0", "v0.4.1-0.20261112-abc+dirty": "0.4.1-0.20261112-abc+dirty"} {
		if got := moduleVersion(raw); got != want {
			t.Fatalf("moduleVersion(%q) = %q, want %q", raw, got, want)
		}
	}
}

func TestStampedIsTheVariables(t *testing.T) {
	saved := Stamped()
	t.Cleanup(func() { Version, Revision, Time = saved.Version, saved.Revision, saved.Time })
	Version, Revision, Time = "1.0.0", "abc", "t"
	if got := Stamped(); got != (Stamp{Version: "1.0.0", Revision: "abc", Time: "t"}) {
		t.Fatalf("Stamped = %+v", got)
	}
}
