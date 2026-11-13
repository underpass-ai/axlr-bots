package filestore

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

// TranscriptStore is application.TranscriptStore on plain files, the one
// a build without SQLite keeps (docs/architecture/transcripts.md): each
// transcript as <state>/transcripts/<execution>/<session>.json, written
// atomically, and its record appended to <state>/transcripts/index.jsonl;
// a replaced transcript appends a new record, and the newest record of an
// execution and session wins.
type TranscriptStore struct {
	dir string

	mu     sync.Mutex
	loaded bool
	last   int64
}

// NewTranscriptStore keeps the transcripts under <stateDir>/transcripts.
func NewTranscriptStore(stateDir string) *TranscriptStore {
	return &TranscriptStore{dir: filepath.Join(stateDir, "transcripts")}
}

func (s *TranscriptStore) indexPath() string { return filepath.Join(s.dir, "index.jsonl") }

func (s *TranscriptStore) contentPath(execution domain.ExecutionID, session string) (string, error) {
	for _, part := range []string{execution.String(), session} {
		if part == "" || strings.ContainsAny(part, `/\`) || strings.Contains(part, "..") {
			return "", domain.ValidationError{Field: "transcript", Reason: "names no file: " + part}
		}
	}
	return filepath.Join(s.dir, execution.String(), session+".json"), nil
}

// records reads the index: the newest record of each execution and
// session, by seq. A line that is not a record (a torn last write) is
// skipped. Called with mu held.
func (s *TranscriptStore) records() ([]application.TranscriptRecord, error) {
	file, err := os.Open(s.indexPath())
	if errors.Is(err, os.ErrNotExist) {
		s.loaded = true
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer file.Close()
	newest := map[string]application.TranscriptRecord{}
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64<<10), 1<<20)
	for scanner.Scan() {
		var record application.TranscriptRecord
		if json.Unmarshal(scanner.Bytes(), &record) != nil || record.Seq <= 0 {
			continue
		}
		if record.Seq > s.last {
			s.last = record.Seq
		}
		key := record.Execution.String() + "/" + record.Session
		if record.Seq > newest[key].Seq {
			newest[key] = record
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	s.loaded = true
	out := make([]application.TranscriptRecord, 0, len(newest))
	for _, record := range newest {
		out = append(out, record)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Seq < out[j].Seq })
	return out, nil
}

// SaveTranscript writes the content, then appends its record.
func (s *TranscriptStore) SaveTranscript(_ context.Context, record application.TranscriptRecord, content []byte) (application.TranscriptRecord, error) {
	path, err := s.contentPath(record.Execution, record.Session)
	if err != nil {
		return application.TranscriptRecord{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.loaded {
		if _, err := s.records(); err != nil {
			return application.TranscriptRecord{}, fmt.Errorf("filestore: transcripts: %w", err)
		}
	}
	if err := writeFile(path, content); err != nil {
		return application.TranscriptRecord{}, fmt.Errorf("filestore: transcripts: %w", err)
	}
	record.Seq = s.last + 1
	line, err := json.Marshal(record)
	if err != nil {
		return application.TranscriptRecord{}, err
	}
	index, err := os.OpenFile(s.indexPath(), os.O_WRONLY|os.O_CREATE|os.O_APPEND, fileMode)
	if err != nil {
		return application.TranscriptRecord{}, fmt.Errorf("filestore: transcripts: %w", err)
	}
	_, werr := index.Write(append(line, '\n'))
	serr := index.Sync()
	cerr := index.Close()
	if err := errors.Join(werr, serr, cerr); err != nil {
		return application.TranscriptRecord{}, fmt.Errorf("filestore: transcripts: %w", err)
	}
	s.last = record.Seq
	return record, nil
}

// LoadTranscript reads the newest record of the execution and session and
// its content.
func (s *TranscriptStore) LoadTranscript(_ context.Context, execution domain.ExecutionID, session string) (application.TranscriptRecord, []byte, error) {
	path, err := s.contentPath(execution, session)
	if err != nil {
		return application.TranscriptRecord{}, nil, err
	}
	s.mu.Lock()
	records, err := s.records()
	s.mu.Unlock()
	if err != nil {
		return application.TranscriptRecord{}, nil, fmt.Errorf("filestore: transcripts: %w", err)
	}
	for _, record := range records {
		if record.Execution == execution && record.Session == session {
			content, err := os.ReadFile(path)
			if err != nil {
				return application.TranscriptRecord{}, nil, fmt.Errorf("filestore: transcripts: %w", err)
			}
			return record, content, nil
		}
	}
	return application.TranscriptRecord{}, nil, domain.NotFoundError{Kind: "transcript", ID: execution.String() + "/" + session}
}

// ListTranscripts lists the newest records after the cursor.
func (s *TranscriptStore) ListTranscripts(_ context.Context, after int64, limit int) ([]application.TranscriptRecord, error) {
	s.mu.Lock()
	records, err := s.records()
	s.mu.Unlock()
	if err != nil {
		return nil, fmt.Errorf("filestore: transcripts: %w", err)
	}
	out := []application.TranscriptRecord{}
	for _, record := range records {
		if record.Seq > after && len(out) < limit {
			out = append(out, record)
		}
	}
	return out, nil
}
