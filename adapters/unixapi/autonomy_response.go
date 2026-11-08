package unixapi

import (
	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

// AutonomyResponse is a runbook's standing as the autonomy routes and an
// incident answer it: its record, the thresholds of its ladder and,
// through an incident, the verdict on that incident's diagnosis this
// cycle. Saved is false until its first signal comes.
type AutonomyResponse struct {
	domain.RunbookAutonomySnapshot
	Thresholds    *domain.AutonomyThresholdsSnapshot `json:"thresholds,omitempty"`
	DiagnosisMark string                             `json:"diagnosisMark,omitempty"`
	Saved         bool                               `json:"saved"`
}

func newAutonomyResponse(view application.AutonomyView) AutonomyResponse {
	return AutonomyResponse{RunbookAutonomySnapshot: view.Autonomy.Snapshot(), Thresholds: view.Thresholds, DiagnosisMark: view.DiagnosisMark, Saved: view.Saved}
}
