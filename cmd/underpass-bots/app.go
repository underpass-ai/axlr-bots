package main

import (
	"context"
	"fmt"
	"io"

	"github.com/underpass-ai/underpass-bots/adapters/unixapi"
	"github.com/underpass-ai/underpass-bots/application"
	"github.com/underpass-ai/underpass-bots/compose"
)

// newApp wires the application over the state directory (compose is the
// composition root the daemon shares).
func newApp(stateDir, axlrBin, actor string) (application.Dependencies, error) {
	return compose.Dependencies(stateDir, axlrBin, actor)
}

// newService picks how a command reaches the control plane: through the
// daemon's socket when a daemon answers on it, so a submission reaches
// the running service, and in process over the state files otherwise.
// The in-process path is the Phase 0 command and the fallback that
// spools a task while the service is down; notice says which was taken.
func newService(ctx context.Context, deps application.Dependencies, socketPath string, notice io.Writer) unixapi.Service {
	if socketPath != "" && unixapi.Reachable(ctx, socketPath) {
		return unixapi.NewClient(socketPath)
	}
	if socketPath != "" && notice != nil {
		fmt.Fprintf(notice, "underpass-bots: no service answers on %s; working on the state directory directly\n", socketPath)
	}
	return unixapi.NewLocal(deps)
}
