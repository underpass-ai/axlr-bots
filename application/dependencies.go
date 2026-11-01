package application

import "errors"

// Dependencies are the ports every use case may need, wired once by the
// composition root. Actor names who is acting, for the audit log;
// OperatorURL is the operator API's base URL (https://host:7879), for
// the approve and deny links a notification carries, and may be empty.
type Dependencies struct {
	Bots        BotRepository
	Tasks       TaskRepository
	Incidents   IncidentRepository
	Executions  ExecutionRepository
	Timers      TimerRepository
	Nodes       NodeRepository
	Enrollments EnrollmentRepository
	Certifier   Certifier
	Denylist    CertificateDenylist
	Events      EventPublisher
	EventReader EventReader
	Audit       AuditSink
	Runtime     RuntimeExecutor
	Decisions   DecisionExecutor
	Traces      TraceLocator
	Artifacts   ArtifactStore
	Workspace   Workspace
	Deliveries  DeliveryRepository
	Notifier    NotificationSender
	Clock       Clock
	IDs         IDGenerator
	Actor       string
	OperatorURL string
}

// Validate reports the first missing dependency.
func (d Dependencies) Validate() error {
	switch {
	case d.Bots == nil:
		return errors.New("application: BotRepository is required")
	case d.Tasks == nil:
		return errors.New("application: TaskRepository is required")
	case d.Incidents == nil:
		return errors.New("application: IncidentRepository is required")
	case d.Executions == nil:
		return errors.New("application: ExecutionRepository is required")
	case d.Timers == nil:
		return errors.New("application: TimerRepository is required")
	case d.Nodes == nil:
		return errors.New("application: NodeRepository is required")
	case d.Enrollments == nil:
		return errors.New("application: EnrollmentRepository is required")
	case d.Certifier == nil:
		return errors.New("application: Certifier is required")
	case d.Denylist == nil:
		return errors.New("application: CertificateDenylist is required")
	case d.Events == nil:
		return errors.New("application: EventPublisher is required")
	case d.EventReader == nil:
		return errors.New("application: EventReader is required")
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
	case d.Workspace == nil:
		return errors.New("application: Workspace is required")
	case d.Deliveries == nil:
		return errors.New("application: DeliveryRepository is required")
	case d.Notifier == nil:
		return errors.New("application: NotificationSender is required")
	case d.Clock == nil:
		return errors.New("application: Clock is required")
	case d.IDs == nil:
		return errors.New("application: IDGenerator is required")
	case d.Actor == "":
		return errors.New("application: Actor is required")
	}
	return nil
}
