package filestore

import (
	"bytes"
	"context"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/underpass-ai/axlr-bots/domain"
)

// quarantined reports whether the record id of the directory was set
// aside, and checks it was set aside once.
func quarantined(t *testing.T, dir, id string) bool {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(dir, quarantineDirName, id+".json.*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) > 1 {
		t.Errorf("%s set aside %d times", id, len(matches))
	}
	return len(matches) == 1
}

// TestListingSetsACorruptRecordAside: a file that is not JSON no longer
// hides every bot; the listing moves it under .quarantine, says so once
// in the log and answers the good bots, and a get of it is not found.
func TestListingSetsACorruptRecordAside(t *testing.T) {
	var logs bytes.Buffer
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	ctx := context.Background()
	state := t.TempDir()
	repo := NewBotRepository(state)
	for _, id := range []string{"alpha", "omega"} {
		if err := repo.Save(ctx, sampleBot(t, id)); err != nil {
			t.Fatal(err)
		}
	}
	dir := filepath.Join(state, "bots")
	if err := os.WriteFile(filepath.Join(dir, "broken.json"), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Get(ctx, "broken"); err == nil || !strings.Contains(err.Error(), "not valid JSON") {
		t.Errorf("get of a corrupt file: %v", err)
	}
	list, err := repo.List(ctx)
	if err != nil || len(list) != 2 || list[0].ID() != "alpha" || list[1].ID() != "omega" {
		t.Fatalf("list past a corrupt file = %v %d", err, len(list))
	}
	if !quarantined(t, dir, "broken") {
		t.Error("the corrupt file was not set aside")
	}
	if _, err := repo.Get(ctx, "broken"); !domain.IsNotFound(err) {
		t.Errorf("get once set aside: %v", err)
	}
	if again, err := repo.List(ctx); err != nil || len(again) != 2 {
		t.Errorf("second list = %v %d", err, len(again))
	}
	if strings.Count(logs.String(), "set aside") != 1 || !strings.Contains(logs.String(), "bot broken") {
		t.Errorf("log = %q", logs.String())
	}
}

func TestSkipListedLeavesOtherErrorsToTheListing(t *testing.T) {
	var logs bytes.Buffer
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	state := t.TempDir()
	c := collection{dir: filepath.Join(state, "bots"), kind: "bot"}
	if c.skipListed("x", nil) {
		t.Error("a read that worked was skipped")
	}
	if c.skipListed("x", os.ErrPermission) {
		t.Error("an unreadable file was skipped")
	}
	if !c.skipListed("x", domain.NotFoundError{Kind: "bot", ID: "x"}) {
		t.Error("a record gone since the listing was not skipped")
	}
	invalid := invalidRecordError{kind: "bot", id: "x", err: os.ErrInvalid}
	// Gone before it could be set aside (another listing took it).
	if !c.skipListed("x", invalid) {
		t.Error("a corrupt record another listing set aside was not skipped")
	}
	if c.skipListed("../x", invalid) {
		t.Error("an id that is not a file name was moved")
	}
	// The quarantine cannot be made: the listing fails loudly instead.
	blocked := collection{dir: filepath.Join(state, "blocked"), kind: "bot"}
	if err := os.MkdirAll(blocked.dir, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(blocked.dir, quarantineDirName), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if blocked.skipListed("x", invalid) {
		t.Error("skipped a corrupt record that could not be set aside")
	}
	// The rename fails for another reason than a missing file.
	if err := os.MkdirAll(filepath.Join(c.dir, "y.json", "child"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(c.dir, quarantineDirName), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(c.dir, quarantineDirName), 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(filepath.Join(c.dir, quarantineDirName), 0o750) })
	if os.Geteuid() != 0 && c.skipListed("y", invalidRecordError{kind: "bot", id: "y", err: os.ErrInvalid}) {
		t.Error("skipped a corrupt record whose rename failed")
	}
	if !strings.Contains(logs.String(), "cannot set it aside") {
		t.Errorf("log = %q", logs.String())
	}
}
