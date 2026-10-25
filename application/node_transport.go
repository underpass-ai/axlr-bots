package application

import (
	"context"
	"io"
	"time"

	"github.com/underpass-ai/axlr-bots/domain"
)

// NodeTransport is how a node agent speaks to the control plane, always
// outbound from the node: it registers, heartbeats, asks for the next
// execution (a long poll), renews its lease, uploads the artifacts the
// execution delivered and reports the result; while it runs, the
// execution may ask another bot to do a task and wait for it. One adapter speaks
// HTTP+JSON over a Unix socket or loopback; TLS with client
// certificates is a transport concern the messages do not see.
type NodeTransport interface {
	Register(ctx context.Context, registration NodeRegistration) (domain.NodeSnapshot, error)
	Heartbeat(ctx context.Context, node domain.NodeID) (domain.NodeSnapshot, error)
	// NextAssignment waits up to wait for an execution; false when none.
	NextAssignment(ctx context.Context, node domain.NodeID, wait time.Duration) (Assignment, bool, error)
	RenewLease(ctx context.Context, node domain.NodeID, execution domain.ExecutionID) (time.Time, error)
	// UploadArtifact hands over the bytes of an artifact the execution
	// delivered, before the report; the control plane keeps them only
	// when they match the artifact's size and SHA-256.
	UploadArtifact(ctx context.Context, node domain.NodeID, execution domain.ExecutionID, artifact domain.Artifact, content io.Reader) error
	// Report hands the result over; a repeated report is accepted and
	// answered with Accepted false.
	Report(ctx context.Context, node domain.NodeID, execution domain.ExecutionID, report ExecutionReport) (ReportAck, error)
	// Delegate asks the control plane, for the execution, that another
	// bot do a task; a refusal is an answer, not an error.
	Delegate(ctx context.Context, node domain.NodeID, execution domain.ExecutionID, command DelegateCommand) (DelegationAnswer, error)
	// AwaitDelegation waits up to wait for a child the execution's task
	// delegated to end, and answers its state.
	AwaitDelegation(ctx context.Context, node domain.NodeID, execution domain.ExecutionID, child domain.TaskID, wait time.Duration) (DelegationAnswer, error)
}

// ReportAck is the control plane's answer to a report.
type ReportAck struct {
	Accepted bool
	Outcome  domain.ExecutionOutcome
}
