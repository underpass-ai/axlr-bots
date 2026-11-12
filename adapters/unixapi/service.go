// Package unixapi is the control plane's local API: the same operations
// as a Service interface with two implementations, Local over the use
// cases (what the daemon serves and what the command uses when no daemon
// runs) and Client over HTTP on a Unix socket (what the command uses
// when the daemon runs, so a submission reaches the running service
// instead of touching the state files).
package unixapi

import (
	"context"
	"time"

	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

// Service is what an operator can ask the control plane (vision §11.1).
// Bot snapshots come back with their trigger credentials redacted.
type Service interface {
	RegisterBot(ctx context.Context, definition application.BotDefinition) (domain.BotSnapshot, error)
	EnableBot(ctx context.Context, id string) (domain.BotSnapshot, error)
	PauseBot(ctx context.Context, id string) (domain.BotSnapshot, error)
	// StopBot pauses the bot and cancels every task it has not finished.
	StopBot(ctx context.Context, id string) (StopResponse, error)
	// DeleteBot removes the bot: it cancels its unfinished tasks and the
	// delegated children it asked for, resolves its open incidents,
	// removes its timers and answers the bots that still name it. Its
	// history stays.
	DeleteBot(ctx context.Context, id string) (DeleteResponse, error)
	// UpdateBot applies a partial change to the bot's definition.
	UpdateBot(ctx context.Context, id string, patch application.BotPatch) (domain.BotSnapshot, error)
	// StageBot holds a change for later: the patch the update takes,
	// naming its version, checked as the update would check it now.
	StageBot(ctx context.Context, id string, patch application.BotPatch) (domain.BotSnapshot, error)
	// GetStagedBot answers the change the bot holds staged, its secrets
	// redacted.
	GetStagedBot(ctx context.Context, id string) (application.StagedBotView, error)
	// ApplyStagedBot applies the staged change through the update and
	// drops it.
	ApplyStagedBot(ctx context.Context, id string) (domain.BotSnapshot, error)
	// UnstageBot drops the staged change unapplied.
	UnstageBot(ctx context.Context, id string) (domain.BotSnapshot, error)
	ListBots(ctx context.Context) ([]domain.BotSnapshot, error)
	GetBot(ctx context.Context, id string) (domain.BotSnapshot, error)
	// ListGroups returns the teams the bots name, with their members.
	ListGroups(ctx context.Context) ([]application.Group, error)
	// GetGroup returns one team; tenant may be empty when only one tenant
	// has a team of that name.
	GetGroup(ctx context.Context, name, tenant string) (application.Group, error)
	// ListGroupEvents answers a team's timeline after the caller's last
	// event; over the operator address the route may wait for one.
	ListGroupEvents(ctx context.Context, query application.GroupEventsQuery) ([]domain.Event, error)
	// ListGroupTopics answers a team's topics: who publishes, who
	// listens, the last event.
	ListGroupTopics(ctx context.Context, name, tenant string) ([]application.GroupTopic, error)
	// ListGroupLinks answers a team's links in the team channel: the
	// teams it may ask and that may ask it, who asks and who answers.
	ListGroupLinks(ctx context.Context, name, tenant string) ([]application.GroupLink, error)
	// PublishGroupEvent publishes the operator's event on a team; the
	// same key again answers the same event, Created false.
	PublishGroupEvent(ctx context.Context, command application.PublishGroupEventCommand) (application.PublishAnswer, error)
	// ListEvents returns the newest limit events of the bot, oldest first.
	ListEvents(ctx context.Context, id string, limit int) ([]domain.Event, error)
	SubmitTask(ctx context.Context, request SubmitRequest) (SubmitResponse, error)
	// FireTrigger fires a webhook trigger with an event; over the operator
	// address the request authenticates with the trigger's own credential.
	FireTrigger(ctx context.Context, request FireRequest) (FireResponse, error)
	RunTask(ctx context.Context, id string) (RunResponse, error)
	// DecideTask decides the pending call; a decision after its deadline
	// is refused with the kind approval_expired.
	DecideTask(ctx context.Context, id string, request DecideRequest) (RunResponse, error)
	// CancelTask stops a task that has not finished.
	CancelTask(ctx context.Context, id string) (CancelResponse, error)
	// SendTaskMessage takes what the operator writes to a running task;
	// its node hands it to the run between model steps.
	SendTaskMessage(ctx context.Context, id string, request MessageRequest) (application.SentTaskMessage, error)
	GetTask(ctx context.Context, id string) (domain.TaskSnapshot, error)
	ListTasks(ctx context.Context, query application.TaskQuery) ([]domain.TaskSnapshot, error)
	// ListTaskEvents answers a task's timeline after the caller's last
	// event, oldest first.
	ListTaskEvents(ctx context.Context, query application.TaskEventsQuery) ([]domain.Event, error)
	GetOverview(ctx context.Context, since time.Time) (application.Overview, error)
	// OpenIncident reports an alert group: it opens an incident and its
	// triage task, repeats one already open, or reopens one resolved
	// less than the window ago.
	OpenIncident(ctx context.Context, command application.OpenIncidentCommand) (OpenIncidentResponse, error)
	// ListIncidents and GetIncident answer each incident with whether it
	// needs a person now; GetIncident adds its current task and the count
	// of its tasks' approvals.
	ListIncidents(ctx context.Context, query application.IncidentQuery) ([]IncidentResponse, error)
	GetIncident(ctx context.Context, id string) (IncidentResponse, error)
	// ResolveIncident closes an incident; a resolved one is refused.
	ResolveIncident(ctx context.Context, id string, request ResolveRequest) (domain.IncidentSnapshot, error)
	// MarkDiagnosis records a person's verdict on an incident's diagnosis
	// and answers its runbook's standing; ListAutonomy and GetAutonomy read
	// the runbooks on ladders (docs/architecture/autonomy-ladder.md).
	MarkDiagnosis(ctx context.Context, id string, request DiagnosisRequest) (AutonomyResponse, error)
	ListAutonomy(ctx context.Context, query application.AutonomyQuery) ([]AutonomyResponse, error)
	GetAutonomy(ctx context.Context, id string) (AutonomyResponse, error)
	// LowerAutonomy takes a runbook down by a person's hand.
	LowerAutonomy(ctx context.Context, id string, request LowerRequest) (AutonomyResponse, error)
	// ClearAutonomy ends a runbook's outstanding applies by a person's hand.
	ClearAutonomy(ctx context.Context, id string, request ClearRequest) (AutonomyResponse, error)
	// ApprovePostmortem and SendBackPostmortem are a person's answer to
	// an incident's postmortem.
	ApprovePostmortem(ctx context.Context, id string, request PostmortemReviewRequest) (domain.IncidentSnapshot, error)
	SendBackPostmortem(ctx context.Context, id string, request PostmortemReviewRequest) (domain.IncidentSnapshot, error)
	// AddOutput keeps a new incident output target; ListOutputs and
	// RemoveOutput answer targets without their URL, secret or command,
	// with a fingerprint instead.
	AddOutput(ctx context.Context, definition application.OutputDefinition) (application.OutputView, error)
	ListOutputs(ctx context.Context) ([]application.OutputView, error)
	RemoveOutput(ctx context.Context, name string) (application.OutputView, error)
	// ListExecutions returns the task's executions, oldest first.
	ListExecutions(ctx context.Context, id string) ([]domain.ExecutionSnapshot, error)
	GetReport(ctx context.Context, id string) (ReportResponse, error)
	GetExecution(ctx context.Context, id string) (domain.ExecutionSnapshot, error)
	// GetArtifact opens an artifact the control plane keeps for the
	// execution; the caller closes the body.
	GetArtifact(ctx context.Context, execution, name string) (ArtifactContent, error)
	ListNodes(ctx context.Context) ([]domain.NodeSnapshot, error)
	GetNode(ctx context.Context, id string) (domain.NodeSnapshot, error)
	// CreateEnrollmentToken mints a one-time token for a node to come;
	// the token is in the answer and nowhere else.
	CreateEnrollmentToken(ctx context.Context, request EnrollTokenRequest) (EnrollTokenResponse, error)
	// RevokeNode cuts a node off and fails what it was running.
	RevokeNode(ctx context.Context, id string) (RevokeResponse, error)
}
