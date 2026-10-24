package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/underpass-ai/axlr-bots/adapters/daemon"
	"github.com/underpass-ai/axlr-bots/adapters/pki"
	"github.com/underpass-ai/axlr-bots/application"
)

// serve runs the control plane as a service until SIGTERM or SIGINT.
// With --listen it also serves the node protocol over TLS, with a
// certificate the fleet CA (ca init) issues for the host's names and
// client certificates verified against that CA.
func serve(ctx context.Context, app application.Dependencies, stateDir, socketPath string, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("serve", flag.ContinueOnError)
	flags.SetOutput(stderr)
	listen := flags.String("listen", os.Getenv("AXLR_BOTS_LISTEN"), "a TLS address to serve nodes on too, such as 127.0.0.1:7878 or 0.0.0.0:7878 (also AXLR_BOTS_LISTEN); needs ca init")
	names := flags.String("server-names", os.Getenv("AXLR_BOTS_SERVER_NAMES"), "names and addresses nodes dial the control plane by, comma-separated, added to the host's own (also AXLR_BOTS_SERVER_NAMES)")
	tick := flags.Duration("tick", daemon.DefaultTick, "how often to look for Pending tasks")
	reconcileEvery := flags.Duration("reconcile-every", daemon.DefaultReconcileEvery, "how often to fail the tasks a dead run left Running")
	grace := flags.Duration("grace", 0, "reconciliation grace (default: the bot's task timeout plus 1m)")
	if err := flags.Parse(args); err != nil {
		return exitUsage
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(stderr, "axlr-bots: serve takes no arguments")
		return exitUsage
	}
	if socketPath == "" {
		fmt.Fprintln(stderr, "axlr-bots: serve needs a socket (--socket or AXLR_BOTS_SOCKET)")
		return exitUsage
	}
	options := daemon.Options{SocketPath: socketPath, Tick: *tick, ReconcileEvery: *reconcileEvery, Grace: *grace}
	if *listen != "" {
		authority, err := pki.Load(caDir(stateDir))
		if err != nil {
			fmt.Fprintln(stderr, "axlr-bots: serve --listen:", err)
			return exitError
		}
		options.ListenAddr = *listen
		options.TLS = pki.ServerTLSConfig(authority, serverNames(*listen, *names))
	}
	ctx, stop := signal.NotifyContext(ctx, syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	options.Log = log.New(stderr, "", log.LstdFlags)
	if err := daemon.Run(ctx, app, options); err != nil {
		fmt.Fprintln(stderr, "axlr-bots:", err)
		return exitError
	}
	return exitOK
}
