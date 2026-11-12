package application

import (
	"bytes"
	"encoding/json"

	"github.com/underpass-ai/axlr-bots/domain"
)

// stagedVersion is the version a staged patch must name: one, valid.
func stagedVersion(patch BotPatch) (domain.SemanticVersion, error) {
	if patch.Version == nil || *patch.Version == "" {
		return "", domain.ValidationError{Field: "version", Reason: "a staged change names the version it brings"}
	}
	return domain.ParseSemanticVersion("version", *patch.Version)
}

// encodeStagedPatch is the patch as the bot keeps it staged.
func encodeStagedPatch(patch BotPatch) ([]byte, error) {
	return json.Marshal(patch)
}

// decodeStagedPatch reads back the patch a bot keeps staged, as strictly
// as the update route reads one.
func decodeStagedPatch(staged domain.StagedDefinition) (BotPatch, error) {
	var patch BotPatch
	decoder := json.NewDecoder(bytes.NewReader(staged.Change()))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&patch); err != nil {
		return BotPatch{}, domain.ValidationError{Field: "staged change", Reason: "cannot be read as a patch: " + err.Error()}
	}
	return patch, nil
}

// nothingStaged is the refusal of an apply or an unstage on a bot that
// holds no staged change.
func nothingStaged(bot domain.BotID, attempted string) error {
	return domain.StateError{Subject: "bot " + bot.String(), Current: "without a staged change", Attempted: attempted}
}
