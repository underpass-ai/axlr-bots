package kmpchannel

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/underpass-ai/axlr-bots/application"
)

// fakeReader runs this test binary as kmp-mcp in the scenario, over a
// state whose team-b store exists.
func fakeReader(t *testing.T, scenario string) (Reader, string) {
	t.Helper()
	out := filepath.Join(t.TempDir(), "call.json")
	t.Setenv("FAKE_KMP", scenario)
	t.Setenv("FAKE_KMP_OUT", out)
	t.Setenv("KMP_MCP_DATA_DIR", "/home/someone/.local/share/kmp/default")
	state := t.TempDir()
	if err := os.MkdirAll(filepath.Join(state, "kmp", "team-b"), 0o700); err != nil {
		t.Fatal(err)
	}
	return Reader{Binary: os.Args[0], Args: []string{"-test.run=^TestHelperProcess$", "--"}, StateDir: state, Timeout: 10 * time.Second}, out
}

func TestReaderWakesTheKeyInTheTeamsStore(t *testing.T) {
	r, out := fakeReader(t, "wake")
	r.Now = func() time.Time { return time.Date(2026, 11, 12, 9, 0, 0, 0, time.UTC) }
	lines, err := r.ReadContext(context.Background(), "team-b", "ws:s1")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"(observation) Finding F-13 not looked at yet.", "(decision) F-12 is flaky.", "open: (open_loop): triage F-13", "open: a loop without a ref", "next: emit team-b.item.sorted for F-13"}
	if strings.Join(lines, "|") != strings.Join(want, "|") {
		t.Errorf("lines = %q", lines)
	}
	raw, _ := os.ReadFile(out)
	var call struct {
		Store, Viewer string
		Params        struct {
			Name      string
			Arguments struct {
				About  string
				Budget map[string]any
			}
		}
	}
	if err := json.Unmarshal(raw, &call); err != nil {
		t.Fatal(err)
	}
	if call.Store != r.Store("team-b") || call.Viewer != "off" || call.Params.Name != "kmp_wake" || call.Params.Arguments.About != "ws:s1" || call.Params.Arguments.Budget["max_bytes"] != float64(readBytes) {
		t.Errorf("call = %+v", call)
	}
}

func TestReaderSaysWhatItCouldNotRead(t *testing.T) {
	ctx := context.Background()
	missing, _ := fakeReader(t, "wake-missing")
	if _, err := missing.ReadContext(ctx, "team-b", "ws:s1"); !errors.Is(err, application.ErrContextNotFound) {
		t.Errorf("missing: %v", err)
	}
	// A team whose store this node never made: not found, and no store
	// is made by the read.
	if _, err := missing.ReadContext(ctx, "team-a", "ws:s1"); !errors.Is(err, application.ErrContextNotFound) {
		t.Errorf("no store: %v", err)
	}
	if _, err := os.Stat(missing.Store("team-a")); !os.IsNotExist(err) {
		t.Error("the read made a store")
	}
	for scenario, want := range map[string]string{"wake-cut": "KMP cut the read to nothing", "wake-refuse": "unavailable: the store is locked", "wake-bare": "kmp_time refused: no structure", "wake-garbled": "unreadable answer", "exit": "the store is locked"} {
		r, _ := fakeReader(t, scenario)
		if _, err := r.ReadContext(ctx, "team-b", "ws:s1"); err == nil || !strings.Contains(err.Error(), want) || errors.Is(err, application.ErrContextNotFound) {
			t.Errorf("%s: %v", scenario, err)
		}
	}
	if _, err := missing.ReadContext(ctx, "Bad Team", "ws:s1"); err == nil {
		t.Error("a bad team was read")
	}
	if _, err := missing.ReadContext(ctx, "team-b", "no key"); err == nil {
		t.Error("a bad key was read")
	}
}
