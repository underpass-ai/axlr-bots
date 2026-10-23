package main

import (
	"fmt"
	"os"

	"github.com/underpass-ai/underpass-bots/adapters/axlr"
	"github.com/underpass-ai/underpass-bots/adapters/filestore"
	"github.com/underpass-ai/underpass-bots/adapters/jsonl"
	"github.com/underpass-ai/underpass-bots/adapters/system"
	"github.com/underpass-ai/underpass-bots/application"
)

// newApp is the composition root: file-backed repositories, JSONL logs
// and the AXLR executor under the state directory.
func newApp(stateDir, axlrBin, actor string) (application.Dependencies, error) {
	if stateDir == "" {
		return application.Dependencies{}, fmt.Errorf("the state directory must not be empty")
	}
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		return application.Dependencies{}, fmt.Errorf("state directory: %w", err)
	}
	deps := application.Dependencies{
		Bots:       filestore.NewBotRepository(stateDir),
		Tasks:      filestore.NewTaskRepository(stateDir),
		Executions: filestore.NewExecutionRepository(stateDir),
		Events:     jsonl.NewEventLog(stateDir),
		Audit:      jsonl.NewAuditLog(stateDir),
		Runtime:    axlr.NewExecutor(axlrBin, stateDir),
		Clock:      system.Clock{},
		IDs:        system.IDGenerator{},
		Actor:      actor,
	}
	if err := deps.Validate(); err != nil {
		return application.Dependencies{}, err
	}
	return deps, nil
}
