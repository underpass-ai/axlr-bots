package domain

// CorrelationRecord is the correlation of one execution as the runtime
// contract names it (bot_id, task_id, execution_id, trace_id): the ids
// the control plane generated, whether the runtime adapter passed them
// to AXLR (Carried) and the file it wrote them in, kept as evidence.
type CorrelationRecord struct {
	BotID       string `json:"botId"`
	TaskID      string `json:"taskId"`
	ExecutionID string `json:"executionId"`
	TraceID     string `json:"traceId"`
	Carried     bool   `json:"carried"`
	Path        string `json:"path,omitempty"`
}
