// Package unixapi is the control plane's local API: the same operations
// as a Service interface with two implementations, Local over the use
// cases (what the daemon serves and what the command uses when no daemon
// runs) and Client over HTTP on a Unix socket (what the command uses
// when the daemon runs, so a submission reaches the running service
// instead of touching the state files).
package unixapi

import (
	"context"
	"time"

	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

// Service is what an operator can ask the control plane (vision §11.1).
// Bot snapshots come back with their trigger credentials redacted.
type Service interface {
	RegisterBot(ctx context.Context, definition application.BotDefinition) (domain.BotSnapshot, error)
	EnableBot(ctx context.Context, id string) (domain.BotSnapshot, error)
	PauseBot(ctx context.Context, id string) (domain.BotSnapshot, error)
	// UpdateBot applies a partial change to the bot's definition.
	UpdateBot(ctx context.Context, id string, patch application.BotPatch) (domain.BotSnapshot, error)
	ListBots(ctx context.Context) ([]domain.BotSnapshot, error)
	GetBot(ctx context.Context, id string) (domain.BotSnapshot, error)
	// ListEvents returns the newest limit events of the bot, oldest first.
	ListEvents(ctx context.Context, id string, limit int) ([]domain.Event, error)
	SubmitTask(ctx context.Context, request SubmitRequest) (SubmitResponse, error)
	// FireTrigger fires a webhook trigger with an event; over the operator
	// address the request authenticates with the trigger's own credential.
	FireTrigger(ctx context.Context, request FireRequest) (FireResponse, error)
	RunTask(ctx context.Context, id string) (RunResponse, error)
	DecideTask(ctx context.Context, id, decision string) (RunResponse, error)
	// CancelTask stops a task that has not finished.
	CancelTask(ctx context.Context, id string) (CancelResponse, error)
	GetTask(ctx context.Context, id string) (domain.TaskSnapshot, error)
	ListTasks(ctx context.Context, query application.TaskQuery) ([]domain.TaskSnapshot, error)
	ListTaskEvents(ctx context.Context, id string, limit int) ([]domain.Event, error)
	GetOverview(ctx context.Context, since time.Time) (application.Overview, error)
	// ListExecutions returns the task's executions, oldest first.
	ListExecutions(ctx context.Context, id string) ([]domain.ExecutionSnapshot, error)
	GetReport(ctx context.Context, id string) (ReportResponse, error)
	GetExecution(ctx context.Context, id string) (domain.ExecutionSnapshot, error)
	ListNodes(ctx context.Context) ([]domain.NodeSnapshot, error)
	GetNode(ctx context.Context, id string) (domain.NodeSnapshot, error)
	// CreateEnrollmentToken mints a one-time token for a node to come;
	// the token is in the answer and nowhere else.
	CreateEnrollmentToken(ctx context.Context, request EnrollTokenRequest) (EnrollTokenResponse, error)
	// RevokeNode cuts a node off and fails what it was running.
	RevokeNode(ctx context.Context, id string) (RevokeResponse, error)
}
