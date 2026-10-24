// Package unixapi is the control plane's local API: the same operations
// as a Service interface with two implementations, Local over the use
// cases (what the daemon serves and what the command uses when no daemon
// runs) and Client over HTTP on a Unix socket (what the command uses
// when the daemon runs, so a submission reaches the running service
// instead of touching the state files).
package unixapi

import (
	"context"

	"github.com/underpass-ai/underpass-bots/application"
	"github.com/underpass-ai/underpass-bots/domain"
)

// Service is what an operator can ask the control plane.
type Service interface {
	RegisterBot(ctx context.Context, definition application.BotDefinition) (domain.BotSnapshot, error)
	EnableBot(ctx context.Context, id string) (domain.BotSnapshot, error)
	ListBots(ctx context.Context) ([]domain.BotSnapshot, error)
	GetBot(ctx context.Context, id string) (domain.BotSnapshot, error)
	SubmitTask(ctx context.Context, request SubmitRequest) (SubmitResponse, error)
	Alert(ctx context.Context, request AlertRequest) (SubmitResponse, error)
	RunTask(ctx context.Context, id string) (RunResponse, error)
	DecideTask(ctx context.Context, id, decision string) (RunResponse, error)
	GetTask(ctx context.Context, id string) (domain.TaskSnapshot, error)
	GetReport(ctx context.Context, id string) (ReportResponse, error)
	GetExecution(ctx context.Context, id string) (domain.ExecutionSnapshot, error)
}
