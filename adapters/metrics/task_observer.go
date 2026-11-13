package metrics

import (
	"context"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

// TaskObserver decorates the control plane's event publisher: each
// event goes to the inner publisher first, and the ones that say a task
// started, ended or waited, an execution ended, or a run's tool calls
// ended, feed the task metrics of their bot. Observing never fails a
// publish: what the stores cannot answer is left out of the series.
//
// What an execution consumed is read from the stored execution when
// the event that ends it arrives, not from the event. A late report
// (the run reports after a cancel ended its execution) fills in the
// cost the cancel could not know and publishes nothing, so the observer
// is also a collector with no series of its own: at each scrape it
// looks again at the executions that ended with an unknown cost.
type TaskObserver struct {
	inner      application.EventPublisher
	tasks      TaskReader
	executions ExecutionReader
	series     *TaskMetrics
	ledger     *executionLedger
}

// NewTaskObserver decorates inner. Register it as a collector too, for
// the late reports.
func NewTaskObserver(inner application.EventPublisher, tasks TaskReader, executions ExecutionReader, series *TaskMetrics) *TaskObserver {
	return &TaskObserver{inner: inner, tasks: tasks, executions: executions, series: series, ledger: newExecutionLedger()}
}

// Publish hands the event on and then observes it.
func (o *TaskObserver) Publish(ctx context.Context, event domain.Event) error {
	if err := o.inner.Publish(ctx, event); err != nil {
		return err
	}
	o.observe(ctx, event)
	return nil
}

func (o *TaskObserver) observe(ctx context.Context, event domain.Event) {
	switch event.Type {
	case domain.EventTaskStarted:
		o.queued(ctx, event)
	case domain.EventTaskCompleted:
		o.ended(ctx, event, domain.TaskCompleted)
	case domain.EventTaskFailed:
		o.ended(ctx, event, domain.TaskFailed)
	case domain.EventTaskExpired:
		o.ended(ctx, event, domain.TaskExpired)
	case domain.EventTaskCancelled, domain.EventTaskPreempted:
		o.approvalClosed(ctx, event, domain.ApprovalCancelled)
		o.ended(ctx, event, domain.TaskCancelled)
	case domain.EventTaskWaitingApproval, domain.EventExecutionReported:
		o.account(ctx, event.ExecutionID)
	case domain.EventTaskDecided:
		// A decision the deadline took was counted by its expiry.
		if event.Payload["outcome"] != domain.ApprovalExpired {
			o.approvalClosed(ctx, event, "")
		}
	case domain.EventApprovalExpired:
		o.approvalClosed(ctx, event, domain.ApprovalExpired)
	case domain.EventTaskProgress:
		o.toolCalls(event)
	}
}

// queued is how long the task waited for its first execution to start.
func (o *TaskObserver) queued(ctx context.Context, event domain.Event) {
	task, err := o.tasks.Get(ctx, event.TaskID)
	if err != nil {
		return
	}
	execution, err := o.executions.Get(ctx, event.ExecutionID)
	if err != nil {
		return
	}
	o.series.taskQueue.WithLabelValues(string(event.BotID)).Observe(seconds(execution.StartedAt().Sub(task.CreatedAt())))
}

// ended counts a task that ended, how long it lived, and the execution
// that ended with it.
func (o *TaskObserver) ended(ctx context.Context, event domain.Event, state domain.TaskStatus) {
	bot := string(event.BotID)
	o.series.tasksFinished.WithLabelValues(bot, string(state)).Inc()
	if task, err := o.tasks.Get(ctx, event.TaskID); err == nil {
		o.series.taskDuration.WithLabelValues(bot, string(state)).Observe(seconds(event.CreatedAt.Sub(task.CreatedAt())))
	}
	o.account(ctx, event.ExecutionID)
}

// approvalClosed is how long a task waited for a person to decide. The
// wait is the task's own; want names the outcome the event must have
// closed it with (empty: any closed one), so a cancel of a task whose
// wait ended long ago adds nothing.
func (o *TaskObserver) approvalClosed(ctx context.Context, event domain.Event, want string) {
	task, err := o.tasks.Get(ctx, event.TaskID)
	if err != nil {
		return
	}
	wait, ok := task.Approval()
	if !ok || wait.Open() || wait.ClosedAt == nil || (want != "" && wait.Outcome != want) {
		return
	}
	o.series.approvalWait.WithLabelValues(string(event.BotID), wait.Outcome).Observe(seconds(wait.ClosedAt.Sub(wait.Since)))
}

// toolCalls counts the calls a progress event says ended.
func (o *TaskObserver) toolCalls(event domain.Event) {
	bot := string(event.BotID)
	for _, call := range endedToolCalls(event) {
		o.series.toolCalls.WithLabelValues(bot, toolStatus(call.Status)).Inc()
		if call.DurationMS > 0 {
			o.series.toolCallDuration.WithLabelValues(bot).Observe(float64(call.DurationMS) / 1000)
		}
	}
}

// account counts an execution that has ended, once: how long it ran,
// what it consumed or that its cost is unknown. One counted with an
// unknown cost is looked at again for the cost a late report gave.
func (o *TaskObserver) account(ctx context.Context, id domain.ExecutionID) {
	if id == "" {
		return
	}
	counted, unknown := o.ledger.state(id)
	if counted && !unknown {
		return
	}
	execution, err := o.executions.Get(ctx, id)
	if err != nil || !execution.Outcome().IsFinal() {
		return
	}
	usage := execution.Usage()
	bot := string(execution.BotID())
	if counted {
		if !usage.CostUnknown && o.ledger.settle(id) {
			o.addUsage(bot, usage)
		}
		return
	}
	if !o.ledger.count(id, usage.CostUnknown) {
		return
	}
	o.series.executionDuration.WithLabelValues(bot, execution.Outcome().String()).Observe(seconds(execution.EndedAt().Sub(execution.StartedAt())))
	if usage.CostUnknown {
		o.series.costUnknown.WithLabelValues(bot).Inc()
		return
	}
	o.addUsage(bot, usage)
}

func (o *TaskObserver) addUsage(bot string, usage domain.Usage) {
	o.series.costUSD.WithLabelValues(bot).Add(usage.CostUSD)
	for kind, count := range map[string]int{"prompt": usage.PromptTokens, "cached": usage.CachedTokens, "completion": usage.CompletionTokens, "reasoning": usage.ReasoningTokens} {
		if count > 0 {
			o.series.tokens.WithLabelValues(bot, kind).Add(float64(count))
		}
	}
}

// Describe sends nothing: the observer has no series of its own, and a
// collector that describes none is unchecked.
func (o *TaskObserver) Describe(chan<- *prometheus.Desc) {}

// Collect looks again, at each scrape, at the executions counted with
// an unknown cost and counts the cost a late report has given them
// since. It sends no metric.
func (o *TaskObserver) Collect(chan<- prometheus.Metric) {
	ctx, cancel := context.WithTimeout(context.Background(), scrapeTimeout)
	defer cancel()
	for _, id := range o.ledger.unknown() {
		o.account(ctx, id)
	}
}

// seconds is a duration in seconds, never negative: a clock that ran
// backwards between two events is not a negative wait.
func seconds(d time.Duration) float64 { return max(d, 0).Seconds() }
