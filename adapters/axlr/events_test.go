package axlr

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/underpass-ai/axlr-bots/application"
)

func TestATeamRunLoadsTheEmitToolToo(t *testing.T) {
	e, argsFile := fake(t, "result-completed")
	e.Relay = "/usr/local/bin/axlr-node"
	request := delegatingRequest()
	request.Events = &application.EventsOffer{Names: []string{"**"}, MaxSeverity: "warning", Teammates: []string{"quad-1"}}
	request.AllowedTools = append(request.AllowedTools, application.EmitEventToolName)
	if _, err := e.Execute(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(e.StateDir, "mcp", "exec-1.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest delegationManifest
	if err := json.Unmarshal(raw, &manifest); err != nil || strings.Join(manifest.AllowTools, ",") != application.DelegateToolName+","+application.EmitEventToolName {
		t.Errorf("manifest = %s", raw)
	}
	// Only the emit tool: a bot in a team with nobody to ask.
	request.Delegation = nil
	if _, err := e.Execute(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	raw, _ = os.ReadFile(filepath.Join(e.StateDir, "mcp", "exec-1.json"))
	if err := json.Unmarshal(raw, &manifest); err != nil || strings.Join(manifest.AllowTools, ",") != application.EmitEventToolName {
		t.Errorf("emit only = %s", raw)
	}
	if args := strings.Join(readArgs(t, argsFile), " "); !strings.Contains(args, "--approve all") {
		t.Errorf("args = %s", args)
	}
}
