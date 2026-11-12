package main

import (
	"context"
	"fmt"
	"io"

	"github.com/underpass-ai/axlr-bots/adapters/system"
	"github.com/underpass-ai/axlr-bots/adapters/unixapi"
	"github.com/underpass-ai/axlr-bots/domain"
)

// health asks the daemon's socket (or the operator address with
// --control) for /healthz: exit 0 when it answers,
// 1 when it does not. A container's healthcheck runs it, since a minimal
// image has no shell or curl.
func health(ctx context.Context, socketPath string, remote remoteControl, args []string, stdout, stderr io.Writer) int {
	if len(args) != 0 {
		fmt.Fprintln(stderr, "axlr-bots: health takes no arguments")
		return exitUsage
	}
	if remote.enabled() {
		client, err := remote.client()
		if err != nil {
			return fail(stderr, err)
		}
		if err := client.Health(ctx); err != nil {
			fmt.Fprintf(stderr, "axlr-bots: no service answers on %s: %v\n", remote.URL, err)
			return exitError
		}
		fmt.Fprintln(stdout, "ok")
		return exitOK
	}
	if socketPath == "" {
		fmt.Fprintln(stderr, "axlr-bots: health needs a socket (--socket or AXLR_BOTS_SOCKET)")
		return exitUsage
	}
	if !unixapi.Reachable(ctx, socketPath) {
		fmt.Fprintf(stderr, "axlr-bots: no service answers on %s\n", socketPath)
		return exitError
	}
	fmt.Fprintln(stdout, "ok")
	return exitOK
}

// version prints the build this binary is: the release, the commit and
// its time, as GET /v1/build names the running one.
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
