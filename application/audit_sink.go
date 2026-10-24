package application

import (
	"context"

	"github.com/underpass-ai/axlr-bots/domain"
)

// AuditSink keeps who asked for what and how it ended.
type AuditSink interface {
	Record(ctx context.Context, entry domain.AuditEntry) error
}
