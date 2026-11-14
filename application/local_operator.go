package application

import (
	"context"
	"sync"
	"time"

	"github.com/underpass-ai/axlr-bots/domain"
)

// LocalOperator is the Service over the use cases, in process. Runs and
// decisions go through the SerialRunner, one at a time; submissions and
// fires are serialised too, so two deliveries of the same key arriving
// together cannot both pass the idempotency lookup and create two tasks.
type LocalOperator struct {
	deps    Dependencies
	runner  *SerialRunner
	submits sync.Mutex
	changes *ChangeHub
	pushers *pushers
}

// NewLocalOperator wires the service over the dependencies. Every event the use
// cases record also wakes the long polls and the node streams waiting
// on the service, a push about an execution goes to its node through
// the direct adapter and every pusher added, and a task cancelled or
// preempted while the control plane runs it in process has its run
// interrupted, whichever use case ended it (a stop, an urgent task, a
// team's stop.all, as well as a cancel).
func NewLocalOperator(deps Dependencies) *LocalOperator {
	hub, extra := NewChangeHub(), &pushers{}
	var runner *SerialRunner
	if deps.Events != nil {
		interrupt := func(id domain.TaskID) bool { return runner.Interrupt(id) }
		deps.Events = tappedEvents{inner: deps.Events, changes: hub, executions: deps.Executions, pushers: extra, interrupt: interrupt}
	}
	runner = NewSerialRunner(deps)
	return &LocalOperator{deps: deps, runner: runner, changes: hub, pushers: extra}
}

// AddPusher adds a bus adapter the pushes to nodes also go through (a
// broker), beside the direct stream; log hears a push the adapter
// refused.
func (l *LocalOperator) AddPusher(pusher NodePusher, log func(string, ...any)) {
	l.pushers.log = log
	l.pushers.add(pusher)
}

// Pusher is the direct bus adapter: what the control plane pushes to a
// node's stream, for a test of the stream against the bus contract.
func (l *LocalOperator) Pusher() NodePusher { return l.changes }

// Runner is the serial runner the service shares with the daemon's worker.
func (l *LocalOperator) Runner() *SerialRunner { return l.runner }

func (l *LocalOperator) RegisterBot(ctx context.Context, definition BotDefinition) (domain.BotSnapshot, error) {
	bot, err := l.runner.RegisterBot(ctx, definition)
	if err != nil {
		return domain.BotSnapshot{}, err
	}
	return bot.Snapshot().Redacted(), nil
}

func (l *LocalOperator) EnableBot(ctx context.Context, id string) (domain.BotSnapshot, error) {
	botID, err := domain.NewBotID(id)
	if err != nil {
		return domain.BotSnapshot{}, err
	}
	bot, err := l.runner.EnableBot(ctx, botID)
	if err != nil {
		return domain.BotSnapshot{}, err
	}
	return bot.Snapshot().Redacted(), nil
}

func (l *LocalOperator) PauseBot(ctx context.Context, id string) (domain.BotSnapshot, error) {
	botID, err := domain.NewBotID(id)
	if err != nil {
		return domain.BotSnapshot{}, err
	}
	bot, err := l.runner.PauseBot(ctx, botID)
	if err != nil {
		return domain.BotSnapshot{}, err
	}
	return bot.Snapshot().Redacted(), nil
}

func (l *LocalOperator) UpdateBot(ctx context.Context, id string, patch BotPatch) (domain.BotSnapshot, error) {
	botID, err := domain.NewBotID(id)
	if err != nil {
		return domain.BotSnapshot{}, err
	}
	bot, err := l.runner.UpdateBot(ctx, botID, patch)
	if err != nil {
		return domain.BotSnapshot{}, err
	}
	return bot.Snapshot().Redacted(), nil
}

func (l *LocalOperator) StageBot(ctx context.Context, id string, patch BotPatch) (domain.BotSnapshot, error) {
	botID, err := domain.NewBotID(id)
	if err != nil {
		return domain.BotSnapshot{}, err
	}
	bot, err := l.runner.StageBot(ctx, botID, patch)
	if err != nil {
		return domain.BotSnapshot{}, err
	}
	return bot.Snapshot().Redacted(), nil
}

func (l *LocalOperator) GetStagedBot(ctx context.Context, id string) (StagedBotView, error) {
	botID, err := domain.NewBotID(id)
	if err != nil {
		return StagedBotView{}, err
	}
	return NewGetStagedBot(l.deps).Execute(ctx, botID)
}

func (l *LocalOperator) ApplyStagedBot(ctx context.Context, id string) (domain.BotSnapshot, error) {
	botID, err := domain.NewBotID(id)
	if err != nil {
		return domain.BotSnapshot{}, err
	}
	bot, err := l.runner.ApplyStagedBot(ctx, botID)
	if err != nil {
		return domain.BotSnapshot{}, err
	}
	return bot.Snapshot().Redacted(), nil
}

func (l *LocalOperator) UnstageBot(ctx context.Context, id string) (domain.BotSnapshot, error) {
	botID, err := domain.NewBotID(id)
	if err != nil {
		return domain.BotSnapshot{}, err
	}
	bot, err := l.runner.UnstageBot(ctx, botID)
	if err != nil {
		return domain.BotSnapshot{}, err
	}
	return bot.Snapshot().Redacted(), nil
}

func (l *LocalOperator) StopBot(ctx context.Context, id string) (StopResponse, error) {
	botID, err := domain.NewBotID(id)
	if err != nil {
		return StopResponse{}, err
	}
	result, err := l.runner.StopBot(ctx, botID)
	if err != nil {
		return StopResponse{}, err
	}
	response := StopResponse{Bot: result.Bot.Snapshot().Redacted(), Cancelled: []string{}, NotCancelled: []StopRefusal{}}
	for _, id := range result.Cancelled {
		response.Cancelled = append(response.Cancelled, id.String())
	}
	response.NotCancelled = append(response.NotCancelled, result.NotCancelled...)
	return response, nil
}

// DeleteBot deletes the bot under the submissions lock and then the
// runner's: no submission or fire can create a task for it while it
// goes, and no task of it is assigned or settled meanwhile.
func (l *LocalOperator) DeleteBot(ctx context.Context, id string) (DeleteResponse, error) {
	botID, err := domain.NewBotID(id)
	if err != nil {
		return DeleteResponse{}, err
	}
	l.submits.Lock()
	defer l.submits.Unlock()
	result, err := l.runner.DeleteBot(ctx, botID)
	if err != nil {
		return DeleteResponse{}, err
	}
	return DeleteResponse{
		Bot: result.Bot.Snapshot().Redacted(), Cancelled: taskIDs(result.Cancelled), ChildrenCancelled: taskIDs(result.ChildrenCancelled),
		TimersRemoved:        result.TimersRemoved,
		NotificationsDropped: result.NotificationsDropped, NamedBy: result.NamedBy,
	}, nil
}

// TickTimers runs the scheduler's pass under the submissions lock, as a
// fire does, and the runner's.
func (l *LocalOperator) TickTimers(ctx context.Context) (TickTimersResult, error) {
	l.submits.Lock()
	defer l.submits.Unlock()
	return l.runner.TickTimers(ctx)
}

func (l *LocalOperator) ListBots(ctx context.Context) ([]domain.BotSnapshot, error) {
	bots, err := NewListBots(l.deps).Execute(ctx)
	if err != nil {
		return nil, err
	}
	snapshots := make([]domain.BotSnapshot, 0, len(bots))
	for _, b := range bots {
		snapshots = append(snapshots, b.Snapshot().Redacted())
	}
	return snapshots, nil
}

func (l *LocalOperator) GetBot(ctx context.Context, id string) (domain.BotSnapshot, error) {
	botID, err := domain.NewBotID(id)
	if err != nil {
		return domain.BotSnapshot{}, err
	}
	bot, err := NewGetBot(l.deps).Execute(ctx, botID)
	if err != nil {
		return domain.BotSnapshot{}, err
	}
	return bot.Snapshot().Redacted(), nil
}

func (l *LocalOperator) ListGroups(ctx context.Context) ([]Group, error) {
	return NewListGroups(l.deps).Execute(ctx)
}

func (l *LocalOperator) GetGroup(ctx context.Context, name, tenant string) (Group, error) {
	return NewGetGroup(l.deps).Execute(ctx, name, tenant)
}

func (l *LocalOperator) ListGroupEvents(ctx context.Context, query GroupEventsQuery) ([]domain.Event, error) {
	return NewListGroupEvents(l.deps).Execute(ctx, query)
}

func (l *LocalOperator) ListGroupTopics(ctx context.Context, name, tenant string) ([]GroupTopic, error) {
	return NewListGroupTopics(l.deps).Execute(ctx, name, tenant)
}

func (l *LocalOperator) PublishGroupEvent(ctx context.Context, command PublishGroupEventCommand) (PublishAnswer, error) {
	return l.runner.Publish(ctx, command)
}

func (l *LocalOperator) ListEvents(ctx context.Context, id string, limit int) ([]domain.Event, error) {
	botID, err := domain.NewBotID(id)
	if err != nil {
		return nil, err
	}
	return NewListEvents(l.deps).Execute(ctx, botID, limit)
}

func (l *LocalOperator) SubmitTask(ctx context.Context, request SubmitRequest) (SubmitResponse, error) {
	l.submits.Lock()
	defer l.submits.Unlock()
	result, err := l.runner.SubmitTask(ctx, SubmitTaskCommand(request))
	if err != nil {
		return SubmitResponse{}, err
	}
	snapshots, err := l.taskSnapshots(ctx, []domain.Task{result.Task})
	if err != nil {
		return SubmitResponse{}, err
	}
	return SubmitResponse{Created: result.Created, Task: snapshots[0]}, nil
}

func (l *LocalOperator) FireTrigger(ctx context.Context, request FireRequest) (FireResponse, error) {
	l.submits.Lock()
	defer l.submits.Unlock()
	result, err := l.runner.FireTrigger(ctx, FireTriggerCommand{
		BotID: request.BotID, TriggerName: request.Trigger, DeliveryID: request.DeliveryID, Body: request.Body,
	})
	if err != nil {
		return FireResponse{}, err
	}
	response := FireResponse{Created: result.Created, Trigger: result.Trigger, IdempotencyKey: result.IdempotencyKey.String()}
	if result.Task != nil {
		task := result.Task.Snapshot()
		response.Task = &task
	}
	return response, nil
}

// SendTaskMessage records the operator's message for the task's running
// execution, under the runner's state lock.
func (l *LocalOperator) SendTaskMessage(ctx context.Context, id string, request MessageRequest) (SentTaskMessage, error) {
	taskID, err := domain.NewTaskID(id)
	if err != nil {
		return SentTaskMessage{}, err
	}
	return l.runner.SendTaskMessage(ctx, taskID, request.Text)
}

// CancelTask ends the task under the runner's state lock, so it never
// ends one a node's report is settling, and, when the control plane
// itself is running it, interrupts the run.
func (l *LocalOperator) CancelTask(ctx context.Context, id string) (CancelResponse, error) {
	taskID, err := domain.NewTaskID(id)
	if err != nil {
		return CancelResponse{}, err
	}
	result, err := l.runner.CancelTask(ctx, taskID)
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

func (l *LocalOperator) ListExecutions(ctx context.Context, id string) ([]domain.ExecutionSnapshot, error) {
	taskID, err := domain.NewTaskID(id)
	if err != nil {
		return nil, err
	}
	executions, err := NewListExecutions(l.deps).Execute(ctx, taskID)
	if err != nil {
		return nil, err
	}
	snapshots := make([]domain.ExecutionSnapshot, 0, len(executions))
	for _, e := range executions {
		snapshots = append(snapshots, e.Snapshot())
	}
	return snapshots, nil
}

func (l *LocalOperator) RunTask(ctx context.Context, id string) (RunResponse, error) {
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

func (l *LocalOperator) DecideTask(ctx context.Context, id string, request DecideRequest) (RunResponse, error) {
	taskID, err := domain.NewTaskID(id)
	if err != nil {
		return RunResponse{}, err
	}
	result, err := l.runner.Decide(ctx, taskID, request.Decision, request.CallID, request.DecidedBy)
	if err != nil {
		return RunResponse{}, err
	}
	return RunResponse{Task: result.Task.Snapshot(), Execution: result.Execution.Snapshot()}, nil
}

func (l *LocalOperator) GetTask(ctx context.Context, id string) (domain.TaskSnapshot, error) {
	taskID, err := domain.NewTaskID(id)
	if err != nil {
		return domain.TaskSnapshot{}, err
	}
	task, err := NewGetTask(l.deps).Execute(ctx, taskID)
	if err != nil {
		return domain.TaskSnapshot{}, err
	}
	snapshots, err := l.taskSnapshots(ctx, []domain.Task{task})
	if err != nil {
		return domain.TaskSnapshot{}, err
	}
	return snapshots[0], nil
}

// ListTasks answers the tasks the query selects, newest first.
func (l *LocalOperator) ListTasks(ctx context.Context, query TaskQuery) ([]domain.TaskSnapshot, error) {
	tasks, err := NewListTasks(l.deps).Execute(ctx, query)
	if err != nil {
		return nil, err
	}
	return l.taskSnapshots(ctx, tasks)
}

// taskSnapshots answers the tasks as plain data, each Pending task of a
// placed bot no node may run saying what it waits for.
func (l *LocalOperator) taskSnapshots(ctx context.Context, tasks []domain.Task) ([]domain.TaskSnapshot, error) {
	waits, err := NewTaskWaits(l.deps).Execute(ctx, tasks)
	if err != nil {
		return nil, err
	}
	snapshots := make([]domain.TaskSnapshot, 0, len(tasks))
	for _, task := range tasks {
		snapshot := task.Snapshot()
		snapshot.Waiting = waits[task.ID()]
		snapshots = append(snapshots, snapshot)
	}
	return snapshots, nil
}

// ListTaskEvents answers the task's timeline after the caller's last
// event, oldest first.
func (l *LocalOperator) ListTaskEvents(ctx context.Context, query TaskEventsQuery) ([]domain.Event, error) {
	taskID, err := domain.NewTaskID(query.ID.String())
	if err != nil {
		return nil, err
	}
	query.ID = taskID
	return NewListTaskEvents(l.deps).Query(ctx, query)
}

// GetOverview answers the counts of the operator's dashboard.
func (l *LocalOperator) GetOverview(ctx context.Context, since time.Time) (Overview, error) {
	return NewGetOverview(l.deps).Execute(ctx, since)
}

func (l *LocalOperator) GetReport(ctx context.Context, id string) (ReportResponse, error) {
	taskID, err := domain.NewTaskID(id)
	if err != nil {
		return ReportResponse{}, err
	}
	task, err := NewGetTask(l.deps).Execute(ctx, taskID)
	if err != nil {
		return ReportResponse{}, err
	}
	content, err := l.deps.Artifacts.LoadReport(ctx, taskID)
	if err != nil {
		return ReportResponse{}, err
	}
	response := ReportResponse{TaskID: id, Content: string(content)}
	if execution, err := l.deps.Executions.Get(ctx, task.CurrentExecutionID()); err == nil {
		response.Reference = ReportReference(execution)
	}
	return response, nil
}

func (l *LocalOperator) GetExecution(ctx context.Context, id string) (domain.ExecutionSnapshot, error) {
	executionID, err := domain.NewExecutionID(id)
	if err != nil {
		return domain.ExecutionSnapshot{}, err
	}
	execution, err := NewGetExecution(l.deps).Execute(ctx, executionID)
	if err != nil {
		return domain.ExecutionSnapshot{}, err
	}
	return execution.Snapshot(), nil
}

// GetArtifact opens an artifact the execution lists as kept.
func (l *LocalOperator) GetArtifact(ctx context.Context, execution, name string) (ArtifactContent, error) {
	executionID, err := domain.NewExecutionID(execution)
	if err != nil {
		return ArtifactContent{}, err
	}
	artifact, body, err := NewGetArtifact(l.deps).Execute(ctx, executionID, name)
	if err != nil {
		return ArtifactContent{}, err
	}
	return ArtifactContent{Artifact: artifact, Body: body}, nil
}

func (l *LocalOperator) ListNodes(ctx context.Context) ([]domain.NodeSnapshot, error) {
	nodes, err := NewListNodes(l.deps).Execute(ctx)
	if err != nil {
		return nil, err
	}
	snapshots := make([]domain.NodeSnapshot, 0, len(nodes))
	for _, n := range nodes {
		snapshots = append(snapshots, n.Snapshot())
	}
	return snapshots, nil
}

func (l *LocalOperator) GetNode(ctx context.Context, id string) (domain.NodeSnapshot, error) {
	nodeID, err := domain.NewNodeID(id)
	if err != nil {
		return domain.NodeSnapshot{}, err
	}
	node, err := NewGetNode(l.deps).Execute(ctx, nodeID)
	if err != nil {
		return domain.NodeSnapshot{}, err
	}
	return node.Snapshot(), nil
}

func (l *LocalOperator) CreateEnrollmentToken(ctx context.Context, request EnrollTokenRequest) (EnrollTokenResponse, error) {
	var ttl time.Duration
	if request.TTL != "" {
		parsed, err := time.ParseDuration(request.TTL)
		if err != nil {
			return EnrollTokenResponse{}, domain.ValidationError{Field: "ttl", Reason: "must be a duration such as 1h"}
		}
		ttl = parsed
	}
	token, err := NewCreateEnrollmentToken(l.deps).Execute(ctx, request.NodeID, request.Labels, ttl)
	if err != nil {
		return EnrollTokenResponse{}, err
	}
	encoded := EnrollTokenCode{ID: token.ID.String(), Secret: token.Secret, NodeID: token.NodeID.String(), CAPEM: string(token.CAPEM)}.Encode()
	return EnrollTokenResponse{Token: encoded, ID: token.ID.String(), NodeID: token.NodeID.String(), Labels: token.Labels, ExpiresAt: token.ExpiresAt}, nil
}

func (l *LocalOperator) RevokeNode(ctx context.Context, id string) (RevokeResponse, error) {
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
func (l *LocalOperator) Dependencies() Dependencies { return l.deps }

// Changes is the hub the node streams and long polls wait on.
func (l *LocalOperator) Changes() *ChangeHub { return l.changes }
