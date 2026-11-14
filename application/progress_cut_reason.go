package application

// ProgressCutReason is why the control plane stopped recording an
// execution's progress timeline.
type ProgressCutReason string

// The reasons a progress timeline is cut.
const (
	// CutEventLimit: the execution reached MaxProgressEvents.
	CutEventLimit ProgressCutReason = "event_limit"
	// CutByteLimit: the items reached MaxProgressBytes.
	CutByteLimit ProgressCutReason = "byte_limit"
	// CutBatchTruncated: a batch lost items to a bound and the last
	// progress event says truncated.
	CutBatchTruncated ProgressCutReason = "batch_truncated"
)
