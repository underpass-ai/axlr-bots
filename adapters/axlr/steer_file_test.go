package axlr

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestSteerAppendsOneLinePerMessageInAPrivateFile(t *testing.T) {
	t.Setenv("FAKE_AXLR_STEER", "1")
	e, _ := fake(t, "result-completed")
	ctx := context.Background()
	if !e.SupportsSteer(ctx) {
		t.Fatal("the binary's --help names --steer-file")
	}
	if err := e.Steer(ctx, "exec-1", []string{"check row F"}); err != nil {
		t.Fatal(err)
	}
	if err := e.Steer(ctx, "exec-1", []string{`and row "G"?`}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(e.StateDir, "steer", "exec-1.jsonl")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSuffix(string(raw), "\n"), "\n")
	var texts []string
	for _, line := range lines {
		var decoded map[string]string
		if err := json.Unmarshal([]byte(line), &decoded); err != nil || len(decoded) != 1 {
			t.Fatalf("line %q: %v %v", line, err, decoded)
		}
		texts = append(texts, decoded["text"])
	}
	if !slices.Equal(texts, []string{"check row F", `and row "G"?`}) {
		t.Errorf("texts = %q", texts)
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("file: %v %v", err, info)
	}
	if info, err := os.Stat(filepath.Dir(path)); err != nil || info.Mode().Perm() != 0o700 {
		t.Errorf("dir: %v %v", err, info)
	}
	if args := e.steerArgs(ctx, "exec-1"); !slices.Equal(args, []string{"--steer-file", path}) {
		t.Errorf("args = %v", args)
	}
	e.dropSteer("exec-1")
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("dropSteer left the file: %v", err)
	}
	// A run that never had a message drops nothing and fails nothing.
	e.dropSteer("exec-2")
}

func TestARunReadsItsSteerFileAndTheFileGoesWithIt(t *testing.T) {
	t.Setenv("FAKE_AXLR_STEER", "1")
	e, argsFile := fake(t, "result-completed")
	// A message that arrived for the run before it ended.
	if err := e.Steer(context.Background(), "exec-1", []string{"check row F"}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Execute(context.Background(), request(time.Minute)); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(e.StateDir, "steer", "exec-1.jsonl")
	if args := strings.Join(readArgs(t, argsFile), " "); !strings.Contains(args, "--steer-file "+path) {
		t.Errorf("args = %s", args)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("the steer file outlived the run: %v", err)
	}
}

func TestSteerWithoutTheFlagIsRefusedAndWritesNothing(t *testing.T) {
	e, argsFile := fake(t, "result-completed")
	ctx := context.Background()
	if e.SupportsSteer(ctx) {
		t.Fatal("a binary without --steer-file was taken to support it")
	}
	if err := e.Steer(ctx, "exec-1", []string{"check row F"}); !errors.Is(err, ErrSteerUnsupported) {
		t.Errorf("steer: %v", err)
	}
	if _, err := os.Stat(filepath.Join(e.StateDir, "steer")); !os.IsNotExist(err) {
		t.Errorf("a steer directory was made: %v", err)
	}
	if args := e.steerArgs(ctx, "exec-1"); args != nil {
		t.Errorf("args = %v", args)
	}
	if _, err := e.Execute(ctx, request(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if args := strings.Join(readArgs(t, argsFile), " "); strings.Contains(args, "--steer-file") {
		t.Errorf("args = %s", args)
	}
}
