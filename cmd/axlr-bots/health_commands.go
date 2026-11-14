package main

import (
	"context"
	"fmt"
	"io"

	"github.com/underpass-ai/axlr-bots/adapters/grpcclient"
	"github.com/underpass-ai/axlr-bots/adapters/system"
	"github.com/underpass-ai/axlr-bots/domain"
)

// health asks the daemon's socket (or the operator address with
// --control) whether it answers: exit 0 when it does, 1 when it does
// not. The probe is the cheapest read of the operator API. A container's
// healthcheck runs it, since a minimal image has no shell or curl.
func health(ctx context.Context, link operatorLink, args []string, stdout, stderr io.Writer) int {
	if len(args) != 0 {
		fmt.Fprintln(stderr, "axlr-bots: health takes no arguments")
		return exitUsage
	}
	if link.Remote.enabled() {
		client, err := link.Remote.grpcClient()
		if err != nil {
			return fail(stderr, err)
		}
		defer client.Close()
		if err := client.Health(ctx); err != nil {
			fmt.Fprintf(stderr, "axlr-bots: no service answers on %s: %v\n", link.Remote.URL, err)
			return exitError
		}
		fmt.Fprintln(stdout, "ok")
		return exitOK
	}
	socket := link.GRPCSocket
	if socket == "" {
		fmt.Fprintln(stderr, "axlr-bots: health needs a socket (--socket or AXLR_BOTS_SOCKET)")
		return exitUsage
	}
	if !grpcclient.Reachable(ctx, socket) {
		if err := refuseOldDaemon(ctx, link); err != nil {
			return fail(stderr, err)
		}
		fmt.Fprintf(stderr, "axlr-bots: no service answers on %s\n", socket)
		return exitError
	}
	fmt.Fprintln(stdout, "ok")
	return exitOK
}

// version prints the build this binary is: the release, the commit and
// its time, as the control plane names the running one.
func version(args []string, stdout, stderr io.Writer) int {
	if len(args) != 0 {
		fmt.Fprintln(stderr, "axlr-bots: version takes no arguments")
		return exitUsage
	}
	fmt.Fprintln(stdout, describeVersion(system.BuildReader{}.Running()))
	return exitOK
}

// describeVersion is one line: axlr-bots <version> (<commit> <time>).
func describeVersion(build domain.BuildIdentity) string {
	text := "axlr-bots"
	if build.Version != "" && build.Version != "(devel)" {
		text += " " + build.Version
	} else {
		text += " (development build)"
	}
	if build.Known() {
		detail := build.Revision
		if build.Time != "" {
			detail += " " + build.Time
		}
		if build.Modified {
			detail += " with changes"
		}
		text += " (" + detail + ")"
	}
	return text
}
