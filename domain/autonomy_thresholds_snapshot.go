package domain

// AutonomyThresholdsSnapshot is what a definition says of a ladder's
// thresholds; zero or absent takes the default.
type AutonomyThresholdsSnapshot struct {
	Diagnoses int `json:"diagnoses,omitempty"`
	Proposals int `json:"proposals,omitempty"`
	Applies   int `json:"applies,omitempty"`
}
