package domain

// LadderSnapshot is a ladder as plain data: an apply trigger's explicit
// opt-in, its runbooks with their bounds and its thresholds.
type LadderSnapshot struct {
	Runbooks   []LadderRunbookSnapshot     `json:"runbooks"`
	Thresholds *AutonomyThresholdsSnapshot `json:"thresholds,omitempty"`
}
