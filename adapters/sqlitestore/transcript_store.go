package sqlitestore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	// The SQLite driver, registered as "sqlite3".
	_ "github.com/mattn/go-sqlite3"

	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

// TranscriptFile is the database's name under the state directory.
const TranscriptFile = "transcripts.db"

const transcriptSchema = `
CREATE TABLE IF NOT EXISTS transcripts (
	seq          INTEGER PRIMARY KEY AUTOINCREMENT,
	execution_id TEXT    NOT NULL,
	session      TEXT    NOT NULL,
	task_id      TEXT    NOT NULL,
	bot_id       TEXT    NOT NULL,
	size         INTEGER NOT NULL,
	sha256       TEXT    NOT NULL,
	filtered     INTEGER NOT NULL DEFAULT 0,
	sent_sha256  TEXT    NOT NULL DEFAULT '',
	received_at  TEXT    NOT NULL,
	content      BLOB    NOT NULL,
	UNIQUE (execution_id, session)
);
CREATE INDEX IF NOT EXISTS transcripts_task ON transcripts (task_id);`

// TranscriptStore is application.TranscriptStore over one SQLite file.
// The file is opened, and made, at the first call, so a command that
// never touches transcripts makes none.
type TranscriptStore struct {
	path string

	mu sync.Mutex
	db *sql.DB
}

// NewTranscriptStore keeps the transcripts in <stateDir>/transcripts.db.
func NewTranscriptStore(stateDir string) *TranscriptStore {
	return &TranscriptStore{path: filepath.Join(stateDir, TranscriptFile)}
}

// open opens the database once: WAL, a busy timeout, and the schema.
func (s *TranscriptStore) open() (*sql.DB, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db != nil {
		return s.db, nil
	}
	// Made by us first, so the file is the owner's alone.
	file, err := os.OpenFile(s.path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, fmt.Errorf("sqlitestore: %w", err)
	}
	_ = file.Close()
	db, err := sql.Open("sqlite3", "file:"+s.path+"?_journal_mode=WAL&_busy_timeout=5000&_txlock=immediate&_synchronous=FULL")
	if err != nil {
		return nil, fmt.Errorf("sqlitestore: %w", err)
	}
	if _, err := db.Exec(transcriptSchema); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("sqlitestore: schema: %w", err)
	}
	s.db = db
	return db, nil
}

// Close closes the database, if it was opened.
func (s *TranscriptStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return nil
	}
	err := s.db.Close()
	s.db = nil
	return err
}

// SaveTranscript replaces any earlier row of the execution and session
// with a new one, so a replaced transcript comes again after the cursor.
func (s *TranscriptStore) SaveTranscript(ctx context.Context, record application.TranscriptRecord, content []byte) (application.TranscriptRecord, error) {
	db, err := s.open()
	if err != nil {
		return application.TranscriptRecord{}, err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return application.TranscriptRecord{}, fmt.Errorf("sqlitestore: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `DELETE FROM transcripts WHERE execution_id = ? AND session = ?`, record.Execution.String(), record.Session); err != nil {
		return application.TranscriptRecord{}, fmt.Errorf("sqlitestore: %w", err)
	}
	if content == nil {
		content = []byte{}
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO transcripts (execution_id, session, task_id, bot_id, size, sha256, filtered, sent_sha256, received_at, content) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		record.Execution.String(), record.Session, record.Task.String(), record.Bot.String(), record.Size, record.SHA256, record.Filtered, record.SentSHA256, record.ReceivedAt.UTC().Format(time.RFC3339Nano), content)
	if err != nil {
		return application.TranscriptRecord{}, fmt.Errorf("sqlitestore: %w", err)
	}
	if record.Seq, err = result.LastInsertId(); err != nil {
		return application.TranscriptRecord{}, fmt.Errorf("sqlitestore: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return application.TranscriptRecord{}, fmt.Errorf("sqlitestore: %w", err)
	}
	return record, nil
}

// LoadTranscript reads one row.
func (s *TranscriptStore) LoadTranscript(ctx context.Context, execution domain.ExecutionID, session string) (application.TranscriptRecord, []byte, error) {
	db, err := s.open()
	if err != nil {
		return application.TranscriptRecord{}, nil, err
	}
	row := db.QueryRowContext(ctx, `SELECT seq, execution_id, session, task_id, bot_id, size, sha256, filtered, sent_sha256, received_at, content FROM transcripts WHERE execution_id = ? AND session = ?`, execution.String(), session)
	var content []byte
	record, err := scanRecord(row.Scan, &content)
	if errors.Is(err, sql.ErrNoRows) {
		return application.TranscriptRecord{}, nil, domain.NotFoundError{Kind: "transcript", ID: execution.String() + "/" + session}
	}
	if err != nil {
		return application.TranscriptRecord{}, nil, err
	}
	return record, content, nil
}

// ListTranscripts lists the rows after the cursor, without their content.
func (s *TranscriptStore) ListTranscripts(ctx context.Context, after int64, limit int) ([]application.TranscriptRecord, error) {
	db, err := s.open()
	if err != nil {
		return nil, err
	}
	rows, err := db.QueryContext(ctx, `SELECT seq, execution_id, session, task_id, bot_id, size, sha256, filtered, sent_sha256, received_at FROM transcripts WHERE seq > ? ORDER BY seq LIMIT ?`, after, limit)
	if err != nil {
		return nil, fmt.Errorf("sqlitestore: %w", err)
	}
	defer rows.Close()
	records := []application.TranscriptRecord{}
	for rows.Next() {
		record, err := scanRecord(rows.Scan, nil)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqlitestore: %w", err)
	}
	return records, nil
}

// scanRecord reads one row's columns, and its content when content is
// not nil.
func scanRecord(scan func(...any) error, content *[]byte) (application.TranscriptRecord, error) {
	var record application.TranscriptRecord
	var execution, task, bot, received string
	dest := []any{&record.Seq, &execution, &record.Session, &task, &bot, &record.Size, &record.SHA256, &record.Filtered, &record.SentSHA256, &received}
	if content != nil {
		dest = append(dest, content)
	}
	if err := scan(dest...); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return record, err
		}
		return record, fmt.Errorf("sqlitestore: %w", err)
	}
	record.Execution, record.Task, record.Bot = domain.ExecutionID(execution), domain.TaskID(task), domain.BotID(bot)
	at, err := time.Parse(time.RFC3339Nano, received)
	if err != nil {
		return record, fmt.Errorf("sqlitestore: received_at %q: %w", received, err)
	}
	record.ReceivedAt = at
	return record, nil
}
