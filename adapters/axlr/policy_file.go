package axlr

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/underpass-ai/axlr-bots/domain"
)

// Names of the runtime's tools the translation looks at.
const toolLocalExec = "local_exec"

// policyFile is the file `axlr-tui --policy` reads: the contract's policy
// object under its wrapper (docs/runtime-contract.md, "Policy per
// execution").
type policyFile struct {
	Policy policyObject `json:"policy"`
}

// runtimeLevel translates the bot's autonomy level into the runtime's.
// AXLR's observe forbids every command, whatever its arguments, so a bot
// that observes through read-only host commands (local_exec narrowed by
// argument rules) runs under act-within-policy: the allow list still
// holds only reads and the commands the rules name, and the deny list
// closes their write forms. Every other level has the same name on both
// sides.
func runtimeLevel(level domain.AutonomyLevel, allowed []string) string {
	if level == domain.AutonomyObserve {
		for _, tool := range allowed {
			if tool == toolLocalExec {
				return string(domain.AutonomyActWithinPolicy)
			}
		}
	}
	return level.String()
}

// renderPolicy builds the runtime's policy: one allow rule per allowed
// tool, replaced by the bot's argument rules when it has any for that
// tool, and the bot's deny rules as they are. An empty allow-list stays
// empty, which the runtime reads as "the level alone decides"; the
// control plane's bots always name their tools.
func renderPolicy(level domain.AutonomyLevel, allowed []string, rules domain.ToolRules) policyObject {
	object := policyObject{Level: runtimeLevel(level, allowed), Tools: policyTools{Allow: []policyRule{}}}
	for _, tool := range allowed {
		if rules.Constrains(domain.ToolName(tool)) {
			continue
		}
		object.Tools.Allow = append(object.Tools.Allow, policyRule{Name: tool})
	}
	for _, rule := range rules.Allow() {
		object.Tools.Allow = append(object.Tools.Allow, policyRule{Name: rule.Tool().String(), Arguments: rule.Arguments()})
	}
	for _, rule := range rules.Deny() {
		object.Tools.Deny = append(object.Tools.Deny, policyRule{Name: rule.Tool().String(), Arguments: rule.Arguments()})
	}
	return object
}

// policyPath is where the execution's policy file is kept, as evidence
// of what the runtime was asked to enforce.
func (e *Executor) policyPath(execution domain.ExecutionID) string {
	return filepath.Join(e.StateDir, "policies", execution.String()+".json")
}

// writePolicy writes the policy file for the execution.
func (e *Executor) writePolicy(execution domain.ExecutionID, object policyObject) (string, error) {
	path := e.policyPath(execution)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	data, err := json.MarshalIndent(policyFile{Policy: object}, "", "  ")
	if err != nil {
		return "", err
	}
	return path, os.WriteFile(path, append(data, '\n'), 0o644)
}
