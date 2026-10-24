package application

import "errors"

// Dependencies are the ports every use case may need, wired once by the
// composition root. Actor names who is acting, for the audit log.
type Dependencies struct {
	Bots       BotRepository
	Tasks      TaskRepository
	Executions ExecutionRepository
	Nodes      NodeRepository
	Events     EventPublisher
	Audit      AuditSink
	Runtime    RuntimeExecutor
	Decisions  DecisionExecutor
	Traces     TraceLocator
	Artifacts  ArtifactStore
	Clock      Clock
	IDs        IDGenerator
	Actor      string
}

// Validate reports the first missing dependency.
func (d Dependencies) Validate() error {
	switch {
	case d.Bots == nil:
		return errors.New("application: BotRepository is required")
	case d.Tasks == nil:
		return errors.New("application: TaskRepository is required")
	case d.Executions == nil:
		return errors.New("application: ExecutionRepository is required")
	case d.Nodes == nil:
		return errors.New("application: NodeRepository is required")
	case d.Events == nil:
		return errors.New("application: EventPublisher is required")
	case d.Audit == nil:
		return errors.New("application: AuditSink is required")
	case d.Runtime == nil:
		return errors.New("application: RuntimeExecutor is required")
	case d.Decisions == nil:
		return errors.New("application: DecisionExecutor is required")
	case d.Traces == nil:
		return errors.New("application: TraceLocator is required")
	case d.Artifacts == nil:
		return errors.New("application: ArtifactStore is required")
	case d.Clock == nil:
		return errors.New("application: Clock is required")
	case d.IDs == nil:
		return errors.New("application: IDGenerator is required")
	case d.Actor == "":
		return errors.New("application: Actor is required")
	}
	return nil
}
