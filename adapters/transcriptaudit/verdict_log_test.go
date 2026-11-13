package transcriptaudit

import (
	"os"
	"path/filepath"
	"testing"
)

func TestVerdictLogKeepsTheCursor(t *testing.T) {
	path := filepath.Join(t.TempDir(), VerdictFileName)
	log, err := OpenVerdictLog(path)
	if err != nil || log.Cursor() != 0 {
		t.Fatalf("open: %v %d", err, log.Cursor())
	}
	for _, seq := range []int{3, 7, 5} {
		if err := log.Append(VerdictLine{Seq: seq, Verdict: OptionClean}); err != nil {
			t.Fatal(err)
		}
	}
	if log.Cursor() != 7 {
		t.Fatalf("cursor %d", log.Cursor())
	}
	f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	_, _ = f.WriteString(`{"seq": 9, "torn`)
	_ = f.Close()
	again, err := OpenVerdictLog(path)
	if err != nil || again.Cursor() != 7 {
		t.Fatalf("reopen: %v %d", err, again.Cursor())
	}
	if info, _ := os.Stat(path); info.Mode().Perm() != 0o600 {
		t.Errorf("mode %v", info.Mode())
	}
	if _, err := OpenVerdictLog(t.TempDir()); err == nil {
		t.Error("a directory opened as a log")
	}
	bad := &VerdictLog{path: filepath.Join(t.TempDir(), "no", "x")}
	if err := bad.Append(VerdictLine{Seq: 1}); err == nil {
		t.Error("append in a missing directory")
	}
}
