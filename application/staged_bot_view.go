package application

import (
	"time"

	"github.com/underpass-ai/axlr-bots/domain"
)

// StagedBotView is the change a bot holds staged, whole (GET
// /v1/bots/{id}/staged): the version it brings, when it was staged, and
// the patch with its trigger credentials and notification secrets
// redacted, as the bot itself is answered.
type StagedBotView struct {
	Version  string    `json:"version"`
	StagedAt time.Time `json:"stagedAt"`
	Patch    BotPatch  `json:"patch"`
}

// newStagedBotView reads the staged change and redacts its patch.
func newStagedBotView(staged domain.StagedDefinition) (StagedBotView, error) {
	patch, err := decodeStagedPatch(staged)
	if err != nil {
		return StagedBotView{}, err
	}
	if patch.Triggers != nil {
		triggers := make([]domain.TriggerSnapshot, len(*patch.Triggers))
		for i, trigger := range *patch.Triggers {
			triggers[i] = trigger.Redacted()
		}
		patch.Triggers = &triggers
	}
	if patch.Approvals != nil {
		approvals := patch.Approvals.Redacted()
		patch.Approvals = &approvals
	}
	return StagedBotView{Version: staged.Version().String(), StagedAt: staged.StagedAt(), Patch: patch}, nil
}
