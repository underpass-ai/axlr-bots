package axlr

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

// delegationPluginID is the MCP registration id AXLR gives the node's
// delegation server; its tool is application.DelegateToolName.
const delegationPluginID = "axlr-bots"

// delegationManifest is the MCP manifest `axlr-tui --plugin` reads
// (AXLR docs/plugins.md): the node's relay to the execution's socket,
// with the one tool it serves.
type delegationManifest struct {
	ManifestVersion int      `json:"manifest_version"`
	ID              string   `json:"id"`
	Command         string   `json:"command"`
	Args            []string `json:"args"`
	AllowTools      []string `json:"allow_tools"`
}

// wireDelegation writes <state>/mcp/<execution>.json for an execution
// whose bot may delegate and whose node serves the tool, and gives the
// run the flag that loads it. The tool is wired only under an enforced
// policy, where the run's approval level is raised to all: AXLR
// registers a --plugin server with manual approval, the policy's allow
// list still forbids every tool the bot does not name, and
// act-with-approval still asks a person for every allowed call
// (docs/architecture/delegation.md).
func (e *Executor) wireDelegation(execution domain.ExecutionID, offer *application.DelegationOffer, socket string, policy *preparedPolicy) error {
	if offer == nil || socket == "" || e.Relay == "" || !policy.record.Enforced {
		return nil
	}
	if strings.ContainsAny(execution.String(), `/\`) || strings.Contains(execution.String(), "..") {
		return errors.New("the execution id is not a file name")
	}
	manifest := delegationManifest{ManifestVersion: 1, ID: delegationPluginID, Command: e.Relay, Args: []string{"mcp-relay", "--socket", socket}, AllowTools: []string{application.DelegateToolName}}
	folder := filepath.Join(e.StateDir, "mcp")
	if err := os.MkdirAll(folder, 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(folder, execution.String()+".json")
	if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
		return err
	}
	// Its own flags: an --execution bundle replaces --policy, not this.
	policy.plugin = []string{"--plugin", path}
	policy.approve = "all"
	policy.record.Delegation = path
	return nil
}
