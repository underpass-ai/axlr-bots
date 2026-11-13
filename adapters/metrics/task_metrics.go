package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
)

// TaskMetrics are the series the task lifecycle feeds, per bot. No
// series is labelled by a task, an execution or a correlation: a label
// takes the values of a bounded set (bots, states, outcomes, token
// kinds, tool statuses), so the series count does not grow with the
// work.
type TaskMetrics struct {
	taskDuration      *prometheus.HistogramVec
	taskQueue         *prometheus.HistogramVec
	executionDuration *prometheus.HistogramVec
	approvalWait      *prometheus.HistogramVec
	toolCallDuration  *prometheus.HistogramVec
	tasksFinished     *prometheus.CounterVec
	costUSD           *prometheus.CounterVec
	tokens            *prometheus.CounterVec
	costUnknown       *prometheus.CounterVec
	toolCalls         *prometheus.CounterVec
}

// Bounds, in seconds, of what each histogram measures.
var (
	// Tasks wait for a node and for a person, so the long end is a day.
	taskBuckets     = []float64{1, 5, 15, 30, 60, 120, 300, 600, 1800, 3600, 14400, 86400}
	queueBuckets    = []float64{0.1, 0.5, 1, 5, 15, 30, 60, 300, 900, 3600}
	runBuckets      = []float64{1, 5, 15, 30, 60, 120, 300, 600, 1800, 3600}
	approvalBuckets = []float64{10, 60, 300, 900, 3600, 14400, 86400}
	toolBuckets     = []float64{0.05, 0.25, 1, 5, 15, 60, 300}
)

// NewTaskMetrics builds the series. They are a collector, registered
// through the TaskObserver that feeds them.
func NewTaskMetrics() *TaskMetrics {
	histogram := func(name, help string, buckets []float64, labels ...string) *prometheus.HistogramVec {
		return prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: name, Help: help, Buckets: buckets}, labels)
	}
	counter := func(name, help string, labels ...string) *prometheus.CounterVec {
		return prometheus.NewCounterVec(prometheus.CounterOpts{Name: name, Help: help}, labels)
	}
	return &TaskMetrics{
		taskDuration:      histogram("axlr_bots_task_duration_seconds", "Seconds from a task's creation to its end, by the state it ended in (Completed, Failed, Expired, Cancelled).", taskBuckets, "bot", "state"),
		taskQueue:         histogram("axlr_bots_task_queue_seconds", "Seconds a task waited, from its creation to the start of its first execution.", queueBuckets, "bot"),
		executionDuration: histogram("axlr_bots_execution_duration_seconds", "Seconds an execution ran, by how it ended (Completed, Failed, DecisionRequired, Cancelled).", runBuckets, "bot", "outcome"),
		approvalWait:      histogram("axlr_bots_approval_wait_seconds", "Seconds a task waited for a person to decide a call, by how the wait ended (approved, denied, expired, cancelled).", approvalBuckets, "bot", "outcome"),
		toolCallDuration:  histogram("axlr_bots_tool_call_duration_seconds", "Seconds a tool call took, as the run's progress reports it.", toolBuckets, "bot"),
		tasksFinished:     counter("axlr_bots_tasks_finished_total", "Tasks that ended, by the state they ended in.", "bot", "state"),
		costUSD:           counter("axlr_bots_cost_usd_total", "Cost in US dollars of the executions that ended, with the cost a late report gave an execution whose cost was unknown.", "bot"),
		tokens:            counter("axlr_bots_tokens_total", "Tokens the executions that ended consumed, by kind (prompt, cached, completion, reasoning).", "bot", "kind"),
		costUnknown:       counter("axlr_bots_executions_cost_unknown_total", "Executions that ended with an unknown cost (a late report may still tell it).", "bot"),
		toolCalls:         counter("axlr_bots_tool_calls_total", "Tool calls that ended, by status (ok, denied, failed, other).", "bot", "status"),
	}
}

// collectors are every series, as the collectors they are.
func (m *TaskMetrics) collectors() []prometheus.Collector {
	return []prometheus.Collector{m.taskDuration, m.taskQueue, m.executionDuration, m.approvalWait, m.toolCallDuration,
		m.tasksFinished, m.costUSD, m.tokens, m.costUnknown, m.toolCalls}
}

// Describe sends the description of every series.
func (m *TaskMetrics) Describe(ch chan<- *prometheus.Desc) {
	for _, collector := range m.collectors() {
		collector.Describe(ch)
	}
}

// Collect sends every series, one after the other.
func (m *TaskMetrics) Collect(ch chan<- prometheus.Metric) {
	for _, collector := range m.collectors() {
		collector.Collect(ch)
	}
}
