package unixapi

import (
	"context"
	"sync"
	"time"

	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
	"github.com/underpass-ai/axlr-bots/nodeproto"
)

// Local is the Service over the use cases, in process. Runs and
// decisions go through the SerialRunner, one at a time; submissions and
// fires are serialised too, so two deliveries of the same key arriving
// together cannot both pass the idempotency lookup and create two tasks.
type Local struct {
	deps    application.Dependencies
	runner  *application.SerialRunner
	submits sync.Mutex
	changes *changes
	pushers *pushers
}

// NewLocal wires the service over the dependencies. Every event the use
// cases record also wakes the long polls and the node streams waiting
// on the service, and a push about an execution goes to its node through
// the direct adapter and every pusher added.
func NewLocal(deps application.Dependencies) *Local {
	hub, extra := newChanges(), &pushers{}
	if deps.Events != nil {
		deps.Events = tappedEvents{inner: deps.Events, changes: hub, executions: deps.Executions, pushers: extra}
	}
	return &Local{deps: deps, runner: application.NewSerialRunner(deps), changes: hub, pushers: extra}
}

// AddPusher adds a bus adapter the pushes to nodes also go through (a
// broker), beside the direct stream; log hears a push the adapter
// refused.
func (l *Local) AddPusher(pusher application.NodePusher, log func(string, ...any)) {
	l.pushers.log = log
	l.pushers.add(pusher)
}

// Pusher is the direct bus adapter: what the control plane pushes to a
// node's stream, for a test of the stream against the bus contract.
func (l *Local) Pusher() application.NodePusher { return l.changes }

// Runner is the serial runner the service shares with the daemon's worker.
func (l *Local) Runner() *application.SerialRunner { return l.runner }

func (l *Local) RegisterBot(ctx context.Context, definition application.BotDefinition) (domain.BotSnapshot, error) {
	bot, err := application.NewRegisterBot(l.deps).Execute(ctx, definition)
	if err != nil {
		return domain.BotSnapshot{}, err
	}
	return bot.Snapshot().Redacted(), nil
}

func (l *Local) EnableBot(ctx context.Context, id string) (domain.BotSnapshot, error) {
	botID, err := domain.NewBotID(id)
	if err != nil {
		return domain.BotSnapshot{}, err
	}
	bot, err := application.NewEnableBot(l.deps).Execute(ctx, botID)
	if err != nil {
		return domain.BotSnapshot{}, err
	}
	return bot.Snapshot().Redacted(), nil
}

func (l *Local) PauseBot(ctx context.Context, id string) (domain.BotSnapshot, error) {
	botID, err := domain.NewBotID(id)
	if err != nil {
		return domain.BotSnapshot{}, err
	}
	bot, err := application.NewPauseBot(l.deps).Execute(ctx, botID)
	if err != nil {
		return domain.BotSnapshot{}, err
	}
	return bot.Snapshot().Redacted(), nil
}

func (l *Local) UpdateBot(ctx context.Context, id string, patch application.BotPatch) (domain.BotSnapshot, error) {
	botID, err := domain.NewBotID(id)
	if err != nil {
		return domain.BotSnapshot{}, err
	}
	bot, err := application.NewUpdateBot(l.deps).Execute(ctx, botID, patch)
	if err != nil {
		return domain.BotSnapshot{}, err
	}
	return bot.Snapshot().Redacted(), nil
}

func (l *Local) StopBot(ctx context.Context, id string) (StopResponse, error) {
	botID, err := domain.NewBotID(id)
	if err != nil {
		return StopResponse{}, err
	}
	result, err := application.NewStopBot(l.deps).Execute(ctx, botID)
	if err != nil {
		return StopResponse{}, err
	}
	response := StopResponse{Bot: result.Bot.Snapshot().Redacted(), Cancelled: []string{}, NotCancelled: []application.StopRefusal{}}
	for _, id := range result.Cancelled {
		response.Cancelled = append(response.Cancelled, id.String())
	}
	response.NotCancelled = append(response.NotCancelled, result.NotCancelled...)
	return response, nil
}

func (l *Local) ListBots(ctx context.Context) ([]domain.BotSnapshot, error) {
	bots, err := application.NewListBots(l.deps).Execute(ctx)
	if err != nil {
		return nil, err
	}
	snapshots := make([]domain.BotSnapshot, 0, len(bots))
	for _, b := range bots {
		snapshots = append(snapshots, b.Snapshot().Redacted())
	}
	return snapshots, nil
}

func (l *Local) GetBot(ctx context.Context, id string) (domain.BotSnapshot, error) {
	botID, err := domain.NewBotID(id)
	if err != nil {
		return domain.BotSnapshot{}, err
	}
	bot, err := application.NewGetBot(l.deps).Execute(ctx, botID)
	if err != nil {
		return domain.BotSnapshot{}, err
	}
	return bot.Snapshot().Redacted(), nil
}

func (l *Local) ListGroups(ctx context.Context) ([]application.Group, error) {
	return application.NewListGroups(l.deps).Execute(ctx)
}

func (l *Local) GetGroup(ctx context.Context, name, tenant string) (application.Group, error) {
	return application.NewGetGroup(l.deps).Execute(ctx, name, tenant)
}

func (l *Local) ListGroupEvents(ctx context.Context, query application.GroupEventsQuery) ([]domain.Event, error) {
	return application.NewListGroupEvents(l.deps).Execute(ctx, query)
}

func (l *Local) ListGroupTopics(ctx context.Context, name, tenant string) ([]application.GroupTopic, error) {
	return application.NewListGroupTopics(l.deps).Execute(ctx, name, tenant)
}

func (l *Local) PublishGroupEvent(ctx context.Context, command application.PublishGroupEventCommand) (application.PublishAnswer, error) {
	return l.runner.Publish(ctx, command)
}

func (l *Local) ListEvents(ctx context.Context, id string, limit int) ([]domain.Event, error) {
	botID, err := domain.NewBotID(id)
	if err != nil {
		return nil, err
	}
	return application.NewListEvents(l.deps).Execute(ctx, botID, limit)
}

func (l *Local) SubmitTask(ctx context.Context, request SubmitRequest) (SubmitResponse, error) {
	l.submits.Lock()
	defer l.submits.Unlock()
	result, err := application.NewSubmitTask(l.deps).Execute(ctx, application.SubmitTaskCommand{
		BotID: request.BotID, Objective: request.Objective, IdempotencyKey: request.IdempotencyKey, CorrelationID: request.CorrelationID, Priority: request.Priority,
	})
	if err != nil {
		return SubmitResponse{}, err
	}
	return SubmitResponse{Created: result.Created, Task: result.Task.Snapshot()}, nil
}

func (l *Local) FireTrigger(ctx context.Context, request FireRequest) (FireResponse, error) {
	l.submits.Lock()
	defer l.submits.Unlock()
	result, err := application.NewFireTrigger(l.deps).Execute(ctx, application.FireTriggerCommand{
		BotID: request.BotID, TriggerName: request.Trigger, DeliveryID: request.DeliveryID, Body: request.Body,
	})
	if err != nil {
		return FireResponse{}, err
	}
	return FireResponse{Created: result.Created, Task: result.Task.Snapshot(), Trigger: result.Trigger, IdempotencyKey: result.IdempotencyKey.String()}, nil
}

// SendTaskMessage records the operator's message for the task's running
// execution.
func (l *Local) SendTaskMessage(ctx context.Context, id string, request MessageRequest) (application.SentTaskMessage, error) {
	taskID, err := domain.NewTaskID(id)
	if err != nil {
		return application.SentTaskMessage{}, err
	}
	return application.NewSendTaskMessage(l.deps).Execute(ctx, taskID, request.Text)
}

// CancelTask ends the task and, when the control plane itself is
// running it, interrupts the run.
func (l *Local) CancelTask(ctx context.Context, id string) (CancelResponse, error) {
	taskID, err := domain.NewTaskID(id)
	if err != nil {
		return CancelResponse{}, err
	}
	result, err := application.NewCancelTask(l.deps).Execute(ctx, taskID)
	if err != nil {
		return CancelResponse{}, err
	}
	if result.Changed {
		l.runner.Interrupt(taskID)
	}
	response := CancelResponse{Task: result.Task.Snapshot(), Changed: result.Changed}
	if result.Execution != nil {
		execution := result.Execution.Snapshot()
		response.Execution = &execution
	}
	return response, nil
}

func (l *Local) ListExecutions(ctx context.Context, id string) ([]domain.ExecutionSnapshot, error) {
	taskID, err := domain.NewTaskID(id)
	if err != nil {
		return nil, err
	}
	executions, err := application.NewListExecutions(l.deps).Execute(ctx, taskID)
	if err != nil {
		return nil, err
	}
	snapshots := make([]domain.ExecutionSnapshot, 0, len(executions))
	for _, e := range executions {
		snapshots = append(snapshots, e.Snapshot())
	}
	return snapshots, nil
}

func (l *Local) RunTask(ctx context.Context, id string) (RunResponse, error) {
	taskID, err := domain.NewTaskID(id)
	if err != nil {
		return RunResponse{}, err
	}
	result, err := l.runner.Run(ctx, taskID)
	if err != nil {
		return RunResponse{}, err
	}
	return RunResponse{Task: result.Task.Snapshot(), Execution: result.Execution.Snapshot()}, nil
}

func (l *Local) DecideTask(ctx context.Context, id string, request DecideRequest) (RunResponse, error) {
	taskID, err := domain.NewTaskID(id)
	if err != nil {
		return RunResponse{}, err
	}
	result, err := l.runner.Decide(ctx, taskID, request.Decision, request.CallID)
	if err != nil {
		return RunResponse{}, err
	}
	return RunResponse{Task: result.Task.Snapshot(), Execution: result.Execution.Snapshot()}, nil
}

func (l *Local) GetTask(ctx context.Context, id string) (domain.TaskSnapshot, error) {
	taskID, err := domain.NewTaskID(id)
	if err != nil {
		return domain.TaskSnapshot{}, err
	}
	task, err := application.NewGetTask(l.deps).Execute(ctx, taskID)
	if err != nil {
		return domain.TaskSnapshot{}, err
	}
	return task.Snapshot(), nil
}

// ListTasks answers the tasks the query selects, newest first.
func (l *Local) ListTasks(ctx context.Context, query application.TaskQuery) ([]domain.TaskSnapshot, error) {
	tasks, err := application.NewListTasks(l.deps).Execute(ctx, query)
	if err != nil {
		return nil, err
	}
	snapshots := make([]domain.TaskSnapshot, 0, len(tasks))
	for _, t := range tasks {
		snapshots = append(snapshots, t.Snapshot())
	}
	return snapshots, nil
}

// ListTaskEvents answers the task's timeline, oldest first.
func (l *Local) ListTaskEvents(ctx context.Context, id string, limit int) ([]domain.Event, error) {
	taskID, err := domain.NewTaskID(id)
	if err != nil {
		return nil, err
	}
	return application.NewListTaskEvents(l.deps).Execute(ctx, taskID, limit)
}

// GetOverview answers the counts of the operator's dashboard.
func (l *Local) GetOverview(ctx context.Context, since time.Time) (application.Overview, error) {
	return application.NewGetOverview(l.deps).Execute(ctx, since)
}

// OpenIncident holds the submissions lock: the lookup by group key and
// the save are a read then a write, and two deliveries of one alert
// must make one incident.
func (l *Local) OpenIncident(ctx context.Context, command application.OpenIncidentCommand) (OpenIncidentResponse, error) {
	l.submits.Lock()
	defer l.submits.Unlock()
	result, err := application.NewOpenIncident(l.deps).Execute(ctx, command)
	if err != nil {
		return OpenIncidentResponse{}, err
	}
	response := OpenIncidentResponse{Incident: result.Incident.Snapshot(), Created: result.Created, Reopened: result.Reopened}
	if result.Task != nil {
		task := result.Task.Snapshot()
		response.Task = &task
	}
	return response, nil
}

func (l *Local) ListIncidents(ctx context.Context, query application.IncidentQuery) ([]domain.IncidentSnapshot, error) {
	incidents, err := application.NewListIncidents(l.deps).Execute(ctx, query)
	if err != nil {
		return nil, err
	}
	snapshots := make([]domain.IncidentSnapshot, 0, len(incidents))
	for _, incident := range incidents {
		snapshots = append(snapshots, incident.Snapshot())
	}
	return snapshots, nil
}

func (l *Local) GetIncident(ctx context.Context, id string) (domain.IncidentSnapshot, error) {
	incidentID, err := domain.NewIncidentID(id)
	if err != nil {
		return domain.IncidentSnapshot{}, err
	}
	incident, err := application.NewGetIncident(l.deps).Execute(ctx, incidentID)
	if err != nil {
		return domain.IncidentSnapshot{}, err
	}
	return incident.Snapshot(), nil
}

func (l *Local) ResolveIncident(ctx context.Context, id string, request ResolveRequest) (domain.IncidentSnapshot, error) {
	incidentID, err := domain.NewIncidentID(id)
	if err != nil {
		return domain.IncidentSnapshot{}, err
	}
	incident, err := application.NewResolveIncident(l.deps).Execute(ctx, incidentID, request.Reason)
	if err != nil {
		return domain.IncidentSnapshot{}, err
	}
	return incident.Snapshot(), nil
}

func (l *Local) GetReport(ctx context.Context, id string) (ReportResponse, error) {
	taskID, err := domain.NewTaskID(id)
	if err != nil {
		return ReportResponse{}, err
	}
	task, err := application.NewGetTask(l.deps).Execute(ctx, taskID)
	if err != nil {
		return ReportResponse{}, err
	}
	content, err := l.deps.Artifacts.LoadReport(ctx, taskID)
	if err != nil {
		return ReportResponse{}, err
	}
	response := ReportResponse{TaskID: id, Content: string(content)}
	if execution, err := l.deps.Executions.Get(ctx, task.CurrentExecutionID()); err == nil {
		response.Reference = application.ReportReference(execution)
	}
	return response, nil
}

func (l *Local) GetExecution(ctx context.Context, id string) (domain.ExecutionSnapshot, error) {
	executionID, err := domain.NewExecutionID(id)
	if err != nil {
		return domain.ExecutionSnapshot{}, err
	}
	execution, err := application.NewGetExecution(l.deps).Execute(ctx, executionID)
	if err != nil {
		return domain.ExecutionSnapshot{}, err
	}
	return execution.Snapshot(), nil
}

// GetArtifact opens an artifact the execution lists as kept.
func (l *Local) GetArtifact(ctx context.Context, execution, name string) (ArtifactContent, error) {
	executionID, err := domain.NewExecutionID(execution)
	if err != nil {
		return ArtifactContent{}, err
	}
	artifact, body, err := application.NewGetArtifact(l.deps).Execute(ctx, executionID, name)
	if err != nil {
		return ArtifactContent{}, err
	}
	return ArtifactContent{Artifact: artifact, Body: body}, nil
}

func (l *Local) ListNodes(ctx context.Context) ([]domain.NodeSnapshot, error) {
	nodes, err := application.NewListNodes(l.deps).Execute(ctx)
	if err != nil {
		return nil, err
	}
	snapshots := make([]domain.NodeSnapshot, 0, len(nodes))
	for _, n := range nodes {
		snapshots = append(snapshots, n.Snapshot())
	}
	return snapshots, nil
}

func (l *Local) GetNode(ctx context.Context, id string) (domain.NodeSnapshot, error) {
	nodeID, err := domain.NewNodeID(id)
	if err != nil {
		return domain.NodeSnapshot{}, err
	}
	node, err := application.NewGetNode(l.deps).Execute(ctx, nodeID)
	if err != nil {
		return domain.NodeSnapshot{}, err
	}
	return node.Snapshot(), nil
}

func (l *Local) CreateEnrollmentToken(ctx context.Context, request EnrollTokenRequest) (EnrollTokenResponse, error) {
	var ttl time.Duration
	if request.TTL != "" {
		parsed, err := time.ParseDuration(request.TTL)
		if err != nil {
			return EnrollTokenResponse{}, domain.ValidationError{Field: "ttl", Reason: "must be a duration such as 1h"}
		}
		ttl = parsed
	}
	token, err := application.NewCreateEnrollmentToken(l.deps).Execute(ctx, request.NodeID, request.Labels, ttl)
	if err != nil {
		return EnrollTokenResponse{}, err
	}
	encoded := nodeproto.EnrollToken{ID: token.ID.String(), Secret: token.Secret, NodeID: token.NodeID.String(), CAPEM: string(token.CAPEM)}.Encode()
	return EnrollTokenResponse{Token: encoded, ID: token.ID.String(), NodeID: token.NodeID.String(), Labels: token.Labels, ExpiresAt: token.ExpiresAt}, nil
}

func (l *Local) RevokeNode(ctx context.Context, id string) (RevokeResponse, error) {
	nodeID, err := domain.NewNodeID(id)
	if err != nil {
		return RevokeResponse{}, err
	}
	result, err := l.runner.RevokeNode(ctx, nodeID)
	if err != nil {
		return RevokeResponse{}, err
	}
	failed := make([]string, 0, len(result.FailedTasks))
	for _, task := range result.FailedTasks {
		failed = append(failed, task.String())
	}
	return RevokeResponse{Node: result.Node.Snapshot(), FailedTasks: failed}, nil
}

// Dependencies are the use cases' dependencies, for the node routes and
// the daemon's loops: what they record wakes the waiters too.
func (l *Local) Dependencies() application.Dependencies { return l.deps }
