package application

import (
	"context"
	"strings"
	"time"

	"github.com/underpass-ai/underpass-bots/domain"
)

// SubmitAlert turns a failed systemd unit into a diagnosis task: the
// objective asks for the evidence of vision §12.3 and the idempotency
// key names the unit and the failure window, so repeating the alert
// (a flapping unit, a redelivered event) finds the same task.
type SubmitAlert struct {
	deps Dependencies
}

// NewSubmitAlert wires the use case.
func NewSubmitAlert(deps Dependencies) SubmitAlert {
	return SubmitAlert{deps: deps}
}

// Execute submits the diagnosis task, or returns the one the window
// already has.
func (uc SubmitAlert) Execute(ctx context.Context, command AlertCommand) (SubmitTaskResult, error) {
	unit := strings.TrimSpace(command.Unit)
	if unit == "" {
		return SubmitTaskResult{}, domain.ValidationError{Field: "unit", Reason: "must not be empty"}
	}
	if !strings.Contains(unit, ".") {
		unit += ".service"
	}
	at := command.At
	if at.IsZero() {
		at = uc.deps.Clock.Now()
	}
	at = at.UTC()
	window := command.Window
	if window <= 0 {
		window = DefaultAlertWindow
	}
	windowStart := at.Truncate(window)
	return NewSubmitTask(uc.deps).Execute(ctx, SubmitTaskCommand{
		BotID:          command.BotID,
		Objective:      DiagnosisObjective(unit, command.Host, at, command.UserManager),
		IdempotencyKey: AlertKey(unit, windowStart),
		CorrelationID:  command.CorrelationID,
	})
}

// AlertKey is the idempotency key of a unit's failure window.
func AlertKey(unit string, windowStart time.Time) string {
	return "alert:" + unit + ":" + windowStart.UTC().Format(time.RFC3339)
}
