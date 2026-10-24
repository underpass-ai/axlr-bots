package application

import (
	"context"
	"time"

	"github.com/underpass-ai/axlr-bots/domain"
)

// NodeTransport is how a node agent speaks to the control plane, always
// outbound from the node: it registers, heartbeats, asks for the next
// execution (a long poll), renews its lease and reports the result. One
// adapter speaks HTTP+JSON over a Unix socket or loopback; TLS with
// client certificates is a transport concern the messages do not see.
type NodeTransport interface {
	Register(ctx context.Context, registration NodeRegistration) (domain.NodeSnapshot, error)
	Heartbeat(ctx context.Context, node domain.NodeID) (domain.NodeSnapshot, error)
	// NextAssignment waits up to wait for an execution; false when none.
	NextAssignment(ctx context.Context, node domain.NodeID, wait time.Duration) (Assignment, bool, error)
	RenewLease(ctx context.Context, node domain.NodeID, execution domain.ExecutionID) (time.Time, error)
	// Report hands the result over; a repeated report is accepted and
	// answered with Accepted false.
	Report(ctx context.Context, node domain.NodeID, execution domain.ExecutionID, report ExecutionReport) (ReportAck, error)
}

// ReportAck is the control plane's answer to a report.
type ReportAck struct {
	Accepted bool
	Outcome  domain.ExecutionOutcome
}
