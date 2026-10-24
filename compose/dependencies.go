// Package compose is the composition root shared by the command and the
// daemon: file-backed repositories, JSONL logs, the artifact store, the
// fleet CA and the AXLR executor, all under one state directory.
package compose

import (
	"fmt"
	"os"

	"github.com/underpass-ai/axlr-bots/adapters/axlr"
	"github.com/underpass-ai/axlr-bots/adapters/filestore"
	"github.com/underpass-ai/axlr-bots/adapters/jsonl"
	"github.com/underpass-ai/axlr-bots/adapters/pki"
	"github.com/underpass-ai/axlr-bots/adapters/system"
	"github.com/underpass-ai/axlr-bots/application"
)

// Dependencies wires the application over the state directory. axlrBin
// is the axlr-tui to run (a bare name is looked up on PATH); actor names
// who is acting, for the audit log.
func Dependencies(stateDir, axlrBin, actor string) (application.Dependencies, error) {
	if stateDir == "" {
		return application.Dependencies{}, fmt.Errorf("the state directory must not be empty")
	}
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		return application.Dependencies{}, fmt.Errorf("state directory: %w", err)
	}
	executor := axlr.NewExecutor(axlrBin, stateDir)
	deps := application.Dependencies{
		Bots:        filestore.NewBotRepository(stateDir),
		Tasks:       filestore.NewTaskRepository(stateDir),
		Executions:  filestore.NewExecutionRepository(stateDir),
		Nodes:       filestore.NewNodeRepository(stateDir),
		Enrollments: filestore.NewEnrollmentRepository(stateDir),
		Certifier:   pki.NewCertifier(stateDir),
		Denylist:    filestore.NewCertificateDenylist(stateDir),
		Events:      jsonl.NewEventLog(stateDir),
		Audit:       jsonl.NewAuditLog(stateDir),
		Runtime:     executor,
		Decisions:   executor,
		Traces:      executor,
		Artifacts:   filestore.NewArtifactStore(stateDir),
		Clock:       system.Clock{},
		IDs:         system.IDGenerator{},
		Actor:       actor,
	}
	if err := deps.Validate(); err != nil {
		return application.Dependencies{}, err
	}
	return deps, nil
}
