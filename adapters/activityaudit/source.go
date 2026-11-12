package activityaudit

import (
	"context"
	"time"

	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

// Source is the port the adapter reads the fleet's activity through: the
// operator API over the control plane's socket (unixapi.Client). It never
// reads the state directory.
type Source interface {
	ListGroups(ctx context.Context) ([]application.Group, error)
	// WaitGroupEvents is a team's timeline after the caller's last event,
	// waiting up to wait for one: GET /v1/groups/{name}/events?wait=.
	WaitGroupEvents(ctx context.Context, query application.GroupEventsQuery, wait time.Duration) ([]domain.Event, error)
	ListTasks(ctx context.Context, query application.TaskQuery) ([]domain.TaskSnapshot, error)
	GetTask(ctx context.Context, id string) (domain.TaskSnapshot, error)
	ListExecutions(ctx context.Context, id string) ([]domain.ExecutionSnapshot, error)
	ListTaskEvents(ctx context.Context, query application.TaskEventsQuery) ([]domain.Event, error)
}
