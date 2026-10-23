package jsonl

import (
	"context"
	"path/filepath"

	"github.com/underpass-ai/underpass-bots/domain"
)

// AuditLog records audit entries by appending them to <state>/audit.jsonl.
type AuditLog struct {
	file *appender
}

// NewAuditLog writes to audit.jsonl under the state directory.
func NewAuditLog(stateDir string) *AuditLog {
	return &AuditLog{file: &appender{path: filepath.Join(stateDir, "audit.jsonl")}}
}

// Record appends the entry.
func (l *AuditLog) Record(_ context.Context, entry domain.AuditEntry) error {
	return l.file.append(entry)
}

// Path is the log file.
func (l *AuditLog) Path() string { return l.file.path }
