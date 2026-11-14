package main

import (
	"context"
	"fmt"
	"io"

	"github.com/underpass-ai/axlr-bots/adapters/grpcclient"
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
// operator address when --control names one (remoteControl), else through
// the daemon's socket when a daemon answers on it, so a submission reaches
// the running service, and in process over the state files otherwise.
// The transport is gRPC unless --transport says rest. The in-process
// path is the Phase 0 command and the fallback that spools a task while
// the service is down; notice says which was taken. It holds the state
// directory's lock until release: a daemon that runs but did not answer
// the probe in time (busy, or on another socket) is not raced on its own
// files, the command is refused with the reason.
func newService(ctx context.Context, deps application.Dependencies, stateDir string, link operatorLink, notice io.Writer) (unixapi.Service, func(), error) {
	if link.grpc() {
		if client, ok, err := dialGRPC(ctx, link); err != nil || ok {
			if err != nil {
				return nil, nil, err
			}
			return client, func() { _ = client.Close() }, nil
		}
		if err := refuseOldDaemon(ctx, link); err != nil {
			return nil, nil, err
		}
	} else if client, ok, err := restService(ctx, link); err != nil || ok {
		return client, func() {}, err
	}
	release, err := statelock.Acquire(stateDir)
	if err != nil {
		return nil, nil, err
	}
	if socket := link.daemonSocket(); socket != "" && notice != nil {
		fmt.Fprintf(notice, "axlr-bots: no service answers on %s; working on the state directory directly\n", socket)
	}
	return unixapi.NewLocal(deps), release, nil
}

// refuseOldDaemon is the error for a daemon that answers over HTTP but
// not over gRPC: an older control plane. The command must not work on
// the state files then, two writers on one store, so it stops and says
// what to do. nil when no daemon answers over HTTP either.
func refuseOldDaemon(ctx context.Context, link operatorLink) error {
	if link.Remote.enabled() || link.Socket == "" || !unixapi.Reachable(ctx, link.Socket) {
		return nil
	}
	return fmt.Errorf("a service answers on %s over HTTP but not over gRPC at %s: update the control plane to a version that serves the operator gRPC API (or use --transport rest meanwhile)", link.Socket, link.GRPCSocket)
}

// dialGRPC is the gRPC client of the control plane --control names, or of
// the daemon whose gRPC socket answers; ok is false when neither.
func dialGRPC(ctx context.Context, link operatorLink) (*grpcclient.Client, bool, error) {
	if link.Remote.enabled() {
		client, err := link.Remote.grpcClient()
		return client, err == nil, err
	}
	if link.GRPCSocket == "" || !grpcclient.Reachable(ctx, link.GRPCSocket) {
		return nil, false, nil
	}
	client, err := grpcclient.DialUnix(link.GRPCSocket)
	if err != nil {
		return nil, false, err
	}
	if link.Socket != "" {
		client = client.WithTriggerFirer(restTriggers{socket: link.Socket})
	}
	return client, true, nil
}

// restService is the HTTP client, over the operator address or the socket.
func restService(ctx context.Context, link operatorLink) (unixapi.Service, bool, error) {
	if link.Remote.enabled() {
		client, err := link.Remote.client()
		if err != nil {
			return nil, false, err
		}
		return client, true, nil
	}
	if link.Socket != "" && unixapi.Reachable(ctx, link.Socket) {
		return unixapi.NewClient(link.Socket), true, nil
	}
	return nil, false, nil
}
