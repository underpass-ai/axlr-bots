package unixapi

import (
	"context"

	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

// Local is the Service over the use cases, in process. Runs and
// decisions go through the SerialRunner, one at a time.
type Local struct {
	deps   application.Dependencies
	runner *application.SerialRunner
}

// NewLocal wires the service over the dependencies.
func NewLocal(deps application.Dependencies) *Local {
	return &Local{deps: deps, runner: application.NewSerialRunner(deps)}
}

// Runner is the serial runner the service shares with the daemon's worker.
func (l *Local) Runner() *application.SerialRunner { return l.runner }

func (l *Local) RegisterBot(ctx context.Context, definition application.BotDefinition) (domain.BotSnapshot, error) {
	bot, err := application.NewRegisterBot(l.deps).Execute(ctx, definition)
	if err != nil {
		return domain.BotSnapshot{}, err
	}
	return bot.Snapshot(), nil
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
	return bot.Snapshot(), nil
}

func (l *Local) ListBots(ctx context.Context) ([]domain.BotSnapshot, error) {
	bots, err := application.NewListBots(l.deps).Execute(ctx)
	if err != nil {
		return nil, err
	}
	snapshots := make([]domain.BotSnapshot, 0, len(bots))
	for _, b := range bots {
		snapshots = append(snapshots, b.Snapshot())
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
	return bot.Snapshot(), nil
}

func (l *Local) SubmitTask(ctx context.Context, request SubmitRequest) (SubmitResponse, error) {
	result, err := application.NewSubmitTask(l.deps).Execute(ctx, application.SubmitTaskCommand{
		BotID: request.BotID, Objective: request.Objective, IdempotencyKey: request.IdempotencyKey, CorrelationID: request.CorrelationID,
	})
	if err != nil {
		return SubmitResponse{}, err
	}
	return SubmitResponse{Created: result.Created, Task: result.Task.Snapshot()}, nil
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

func (l *Local) DecideTask(ctx context.Context, id, decision string) (RunResponse, error) {
	taskID, err := domain.NewTaskID(id)
	if err != nil {
		return RunResponse{}, err
	}
	result, err := l.runner.Decide(ctx, taskID, decision)
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

// Dependencies are the use cases' dependencies, for the node routes.
func (l *Local) Dependencies() application.Dependencies { return l.deps }
