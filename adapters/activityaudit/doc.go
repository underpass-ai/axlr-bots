// Package activityaudit is the input adapter that audits the fleet's
// finished tasks and turns each new kind of failure into one event for
// the team the operator names, under the event name the operator names
// (docs/runbooks/activity-audit.md): the DTOs generated from
// docs/reference/activity-audit.asyncapi.json, the extraction of a
// task's failures from what the operator API answers (its executions'
// tool calls and its timeline's progress items), the normalisation that
// makes a failure's signature, the seen-store that publishes each
// signature once, and the watcher that wakes on the teams' timelines.
// It is a process beside the world (docs/architecture/input-adapters.md):
// deterministic, it reads and publishes through the socket only, as the
// operator, and decides nothing.
package activityaudit

//go:generate go run ../../internal/asyncapigen/cmd/asyncapigen -spec ../../docs/reference/activity-audit.asyncapi.json -package activityaudit -out .
