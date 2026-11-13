package metrics

import (
	"context"
	"time"

	"github.com/underpass-ai/axlr-bots/application"
)

// OverviewSource answers the overview the operator API answers; a zero
// moment is the start of today on the control plane's clock.
type OverviewSource interface {
	GetOverview(ctx context.Context, since time.Time) (application.Overview, error)
}
