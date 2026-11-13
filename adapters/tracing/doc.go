// Package tracing turns the life of a task into an OpenTelemetry trace.
//
// It is a decorator of application.EventPublisher: every event goes
// through to the publisher it wraps, and the event that ends a task
// (completed, failed, cancelled, preempted, expired) makes the decorator
// build the task's whole trace at once from what the control plane
// stored (the task, its executions, its timeline) and hand it to the
// tracer. Nothing is kept open in memory: a task that waits hours for a
// person, or a control plane that restarts in the middle of one, loses
// no span, because the spans are built from the stored timestamps after
// the fact (see docs/architecture/tracing.md).
package tracing
