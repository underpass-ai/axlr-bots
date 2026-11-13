package transcriptaudit

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"sync"
)

// VerdictFileName is the adapter's file under its directory.
const VerdictFileName = "verdicts.jsonl"

// VerdictLog is the adapter's own file, one VerdictLine per transcript
// audited, appended and synced; its highest seq is the cursor.
type VerdictLog struct {
	path string

	mu     sync.Mutex
	cursor int64
}

// OpenVerdictLog reads the file, if any, for its cursor. A line that is
// not a verdict (a torn last write) is skipped.
func OpenVerdictLog(path string) (*VerdictLog, error) {
	log := &VerdictLog{path: path}
	file, err := os.Open(path)
	if os.IsNotExist(err) {
		return log, nil
	}
	if err != nil {
		return nil, fmt.Errorf("transcriptaudit: %w", err)
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64<<10), 8<<20)
	for scanner.Scan() {
		var line VerdictLine
		if json.Unmarshal(scanner.Bytes(), &line) == nil && int64(line.Seq) > log.cursor {
			log.cursor = int64(line.Seq)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("transcriptaudit: %s: %w", path, err)
	}
	return log, nil
}

// Cursor is the highest seq recorded.
func (l *VerdictLog) Cursor() int64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.cursor
}

// Append writes one line and syncs it, then moves the cursor.
func (l *VerdictLog) Append(line VerdictLine) error {
	data, err := json.Marshal(line)
	if err != nil {
		return err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	file, err := os.OpenFile(l.path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
	if err != nil {
		return fmt.Errorf("transcriptaudit: %w", err)
	}
	if _, err := file.Write(append(data, '\n')); err != nil {
		_ = file.Close()
		return fmt.Errorf("transcriptaudit: %w", err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return fmt.Errorf("transcriptaudit: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("transcriptaudit: %w", err)
	}
	if int64(line.Seq) > l.cursor {
		l.cursor = int64(line.Seq)
	}
	return nil
}
