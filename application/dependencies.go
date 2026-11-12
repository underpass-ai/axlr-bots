package application

import "errors"

// Dependencies are the ports every use case may need, wired once by the
// composition root. Actor names who is acting, for the audit log;
// OperatorURL is the operator API's base URL (https://host:7879), for
// the approve and deny links a notification carries and the incident
// link of an output, and may be empty.
type Dependencies struct {
	Bots        BotRepository
	Tasks       TaskRepository
	Incidents   IncidentRepository
	Autonomy    AutonomyRepository
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
	// Outputs, OutputDeliveries and OutputSender announce incidents to
	// the control plane's output targets.
	Outputs          OutputRepository
	OutputDeliveries OutputDeliveryRepository
	OutputSender     OutputSender
	// SNS is Amazon SNS as a cloudwatch trigger reaches it.
	SNS         SNSTransport
	Clock       Clock
	IDs         IDGenerator
	Actor       string
	OperatorURL string
	// PanelURL is the base of the panel link an incident output carries
	// (<base>/incidents/<id>); empty leaves the link out unless the
	// target names its own.
	PanelURL string
	// Daemon says the process serves others (axlr-bots serve): Actor is
	// then the control plane's own name, which no decision that names
	// its actor may claim where names count (approvers, two persons).
	Daemon bool
	// ModelGateway and JudgeGateway answer the calls of a node with no
	// key; nil refuses them.
	ModelGateway ModelGateway
	JudgeGateway JudgeGateway
	// DomainEvents carries a domain event to other teams
	// (docs/architecture/team-topics.md); nil: no event crosses, and an
	// emit that would cross says so as a skip.
	DomainEvents DomainEventBus
	// Build tells the running build from the installed one, so a
	// control plane left running over a newer binary stops
	// (CheckBuild); nil: nothing is compared.
	Build BuildReader
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
	case d.Autonomy == nil:
		return errors.New("application: AutonomyRepository is required")
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
	case d.Outputs == nil:
		return errors.New("application: OutputRepository is required")
	case d.OutputDeliveries == nil:
		return errors.New("application: OutputDeliveryRepository is required")
	case d.OutputSender == nil:
		return errors.New("application: OutputSender is required")
	case d.SNS == nil:
		return errors.New("application: SNSTransport is required")
	case d.Clock == nil:
		return errors.New("application: Clock is required")
	case d.IDs == nil:
		return errors.New("application: IDGenerator is required")
	case d.Actor == "":
		return errors.New("application: Actor is required")
	}
	return nil
}
