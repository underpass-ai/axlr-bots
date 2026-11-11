package kmpchannel

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/underpass-ai/axlr-bots/domain"
)

func record() domain.ChannelRecord {
	return domain.ChannelRecord{
		Side: domain.ChannelAnswered, Team: "dev", Other: "qa", Question: "task-q1", Asker: "qa-jefe", Responder: "dev-go",
		Text: "¿Qué tests cubren el pago?", Answered: true, Answer: "Los de payments/.", Status: domain.TaskCompleted,
	}
}

// fakeWriter runs this test binary as kmp-mcp in the scenario.
func fakeWriter(t *testing.T, scenario string) (Writer, string) {
	t.Helper()
	out := filepath.Join(t.TempDir(), "call.json")
	t.Setenv("FAKE_KMP", scenario)
	t.Setenv("FAKE_KMP_OUT", out)
	t.Setenv("KMP_MCP_DATA_DIR", "/home/someone/.local/share/kmp/default")
	t.Setenv("KMP_VIEWER_ADDR", "127.0.0.1:7317")
	return Writer{Binary: os.Args[0], Args: []string{"-test.run=^TestHelperProcess$", "--"}, StateDir: t.TempDir(), Timeout: 10 * time.Second}, out
}

func TestWriterWritesTheRecordIntoTheTeamsStore(t *testing.T) {
	w, out := fakeWriter(t, "ok")
	if err := w.WriteChannelRecord(context.Background(), record()); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	var call struct {
		Store, Viewer string
		Params        struct {
			Name      string
			Arguments struct {
				About, Actor string
				Key          string `json:"idempotency_key"`
				Memories     []struct {
					ID, Kind, Summary, Evidence string
					Labels                      map[string][]string
				}
			}
		}
	}
	if err := json.Unmarshal(raw, &call); err != nil {
		t.Fatal(err)
	}
	// The team's store, never the one the node's environment names.
	if call.Store != filepath.Join(w.StateDir, "kmp", "dev") || call.Viewer != "off" {
		t.Fatalf("store %q viewer %q", call.Store, call.Viewer)
	}
	if info, err := os.Stat(call.Store); err != nil || info.Mode().Perm() != 0o700 {
		t.Fatalf("store dir: %v %v", info, err)
	}
	a := call.Params.Arguments
	if call.Params.Name != "kmp_write_memory" || a.About != "project:dev.canal" || a.Actor != "kmp/dev#canal" || a.Key != "canal:answered:task-q1" {
		t.Fatalf("call %+v", call.Params)
	}
	if len(a.Memories) != 1 {
		t.Fatalf("memories %+v", a.Memories)
	}
	m := a.Memories[0]
	if m.Kind != "observation" || !strings.Contains(m.Summary, "question task-q1") || !strings.Contains(m.Evidence, "¿Qué tests") {
		t.Fatalf("memory %+v", m)
	}
	want := map[string]string{"side": "answered", "team": "qa", "question": "task-q1", "asker": "qa-jefe", "responder": "dev-go", "status": "completed"}
	for key, value := range want {
		if got := m.Labels[key]; len(got) != 1 || got[0] != value {
			t.Fatalf("label %s = %v", key, got)
		}
	}
}

func TestWriterReportsWhatWentWrong(t *testing.T) {
	cases := map[string]string{
		"refuse":   "kmp_write_memory refused: idempotency key 'canal:answered:task-q1' was already accepted with different content",
		"rpcerror": "initialize: rpc error -32600: unsupported protocol",
		"exit":     "the server closed before answering; stderr: kmp-mcp: the store is locked",
		"garbled":  "unreadable result",
	}
	for scenario, want := range cases {
		t.Run(scenario, func(t *testing.T) {
			w, _ := fakeWriter(t, scenario)
			err := w.WriteChannelRecord(context.Background(), record())
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Fatalf("got %v, want %q", err, want)
			}
		})
	}
}

func TestWriterKeepsAWriteTheServerAnsweredBeforeItFailed(t *testing.T) {
	w, _ := fakeWriter(t, "fail-after")
	if err := w.WriteChannelRecord(context.Background(), record()); err != nil {
		t.Fatal(err)
	}
}

func TestWriterGivesUpOnAServerThatHangs(t *testing.T) {
	w, _ := fakeWriter(t, "hang")
	w.Timeout = 300 * time.Millisecond
	start := time.Now()
	err := w.WriteChannelRecord(context.Background(), record())
	if err == nil || !strings.Contains(err.Error(), "deadline") {
		t.Fatalf("got %v", err)
	}
	if time.Since(start) > 5*time.Second {
		t.Fatalf("took %s", time.Since(start))
	}
}

func TestWriterRefusesBeforeStarting(t *testing.T) {
	w := Writer{Binary: filepath.Join(t.TempDir(), "no-kmp-mcp"), StateDir: t.TempDir()}
	bad := record()
	bad.Team = "qa"
	if err := w.WriteChannelRecord(context.Background(), bad); err == nil {
		t.Fatal("own team written")
	}
	if err := w.WriteChannelRecord(context.Background(), record()); err == nil {
		t.Fatal("a missing binary wrote")
	}
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	w.StateDir = file
	if err := w.WriteChannelRecord(context.Background(), record()); err == nil {
		t.Fatal("a store under a file written")
	}
}

func TestActorAndStore(t *testing.T) {
	if Actor("qa") != "kmp/qa#canal" {
		t.Fatal(Actor("qa"))
	}
	if (Writer{StateDir: "/s"}).Store("qa") != "/s/kmp/qa" {
		t.Fatal("store")
	}
	if got := withoutKMP([]string{"HOME=/h", "KMP_MCP_DATA_DIR=/x", "KMP_VIEWER_ADDR=1"}); len(got) != 1 || got[0] != "HOME=/h" {
		t.Fatal(got)
	}
}

// TestWriterAgainstKMP writes into a real kmp-mcp's store, with a home
// and a state of its own, when the host has kmp-mcp; skipped otherwise.
func TestWriterAgainstKMP(t *testing.T) {
	binary, err := exec.LookPath("kmp-mcp")
	if err != nil {
		t.Skip("kmp-mcp is not installed")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	for _, name := range []string{"XDG_DATA_HOME", "XDG_CONFIG_HOME", "XDG_STATE_HOME", "XDG_CACHE_HOME"} {
		t.Setenv(name, filepath.Join(home, name))
	}
	t.Setenv("KMP_MCP_DATA_DIR", "")
	w := Writer{Binary: binary, StateDir: t.TempDir()}
	for range 2 { // the second is KMP's replay
		if err := w.WriteChannelRecord(context.Background(), record()); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command(binary, "memories", "--json")
	cmd.Env = append(withoutKMP(os.Environ()), "KMP_MCP_DATA_DIR="+w.Store("dev"))
	listed, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	var report struct {
		Memories []struct {
			Path   string
			Abouts []struct {
				About  string
				Events int
			}
		}
	}
	if err := json.Unmarshal(listed, &report); err != nil {
		t.Fatal(err)
	}
	for _, memory := range report.Memories {
		if memory.Path != w.Store("dev") {
			continue
		}
		for _, about := range memory.Abouts {
			if about.About == "project:dev.canal" && about.Events == 1 {
				return
			}
		}
		t.Fatalf("store %s holds %+v", memory.Path, memory.Abouts)
	}
	t.Fatalf("no store %s in %s", w.Store("dev"), listed)
}
