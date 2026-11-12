package main

import (
	"context"
	"fmt"
	"io"

	"github.com/underpass-ai/axlr-bots/adapters/unixapi"
	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/compose"
	"github.com/underpass-ai/axlr-bots/internal/statelock"
)

// newApp wires the application over the state directory (compose is the
// composition root the daemon shares).
func newApp(stateDir, axlrBin, actor string) (application.Dependencies, error) {
	return compose.Dependencies(stateDir, axlrBin, actor)
}

// newService picks how a command reaches the control plane: through its
// operator address when --control names one (remoteControl), else through the
// daemon's socket when a daemon answers on it, so a submission reaches
// the running service, and in process over the state files otherwise.
// The in-process path is the Phase 0 command and the fallback that
// spools a task while the service is down; notice says which was taken.
// It holds the state directory's lock until release: a daemon that runs
// but did not answer the probe in time (busy, or on another socket) is
// not raced on its own files, the command is refused with the reason.
func newService(ctx context.Context, deps application.Dependencies, stateDir, socketPath string, remote remoteControl, notice io.Writer) (unixapi.Service, func(), error) {
	if remote.enabled() {
		client, err := remote.client()
		if err != nil {
			return nil, nil, err
		}
		return client, func() {}, nil
	}
	if socketPath != "" && unixapi.Reachable(ctx, socketPath) {
		return unixapi.NewClient(socketPath), func() {}, nil
	}
	release, err := statelock.Acquire(stateDir)
	if err != nil {
		return nil, nil, err
	}
	if socketPath != "" && notice != nil {
		fmt.Fprintf(notice, "axlr-bots: no service answers on %s; working on the state directory directly\n", socketPath)
	}
	return unixapi.NewLocal(deps), release, nil
}
