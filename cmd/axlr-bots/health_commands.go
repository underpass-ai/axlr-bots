package main

import (
	"context"
	"fmt"
	"io"

	"github.com/underpass-ai/axlr-bots/adapters/grpcclient"
	"github.com/underpass-ai/axlr-bots/adapters/system"
	"github.com/underpass-ai/axlr-bots/adapters/unixapi"
	"github.com/underpass-ai/axlr-bots/domain"
)

// health asks the daemon's socket (or the operator address with
// --control) whether it answers: exit 0 when it does, 1 when it does
// not. Over gRPC the probe is the cheapest read of the operator API;
// over REST it is /healthz. A container's healthcheck runs it, since a
// minimal image has no shell or curl.
func health(ctx context.Context, link operatorLink, args []string, stdout, stderr io.Writer) int {
	if len(args) != 0 {
		fmt.Fprintln(stderr, "axlr-bots: health takes no arguments")
		return exitUsage
	}
	if link.Remote.enabled() {
		client, err := remoteHealth(link)
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
	socket := link.daemonSocket()
	if socket == "" {
		fmt.Fprintln(stderr, "axlr-bots: health needs a socket (--socket or AXLR_BOTS_SOCKET)")
		return exitUsage
	}
	answers := unixapi.Reachable
	if link.grpc() {
		answers = grpcclient.Reachable
	}
	if !answers(ctx, socket) {
		if link.grpc() {
			if err := refuseOldDaemon(ctx, link); err != nil {
				return fail(stderr, err)
			}
		}
		fmt.Fprintf(stderr, "axlr-bots: no service answers on %s\n", socket)
		return exitError
	}
	fmt.Fprintln(stdout, "ok")
	return exitOK
}

// remoteHealthClient asks a remote control plane whether it answers.
type remoteHealthClient interface {
	Health(ctx context.Context) error
	Close() error
}

// restHealth is the HTTP client, which holds no connection to close.
type restHealth struct {
	*unixapi.Client
}

func (restHealth) Close() error { return nil }

// remoteHealth is the client of the remote by the transport; an error is
// a remote that could not even be dialed (a missing token file, a bad
// CA).
func remoteHealth(link operatorLink) (remoteHealthClient, error) {
	if link.grpc() {
		return link.Remote.grpcClient()
	}
	client, err := link.Remote.client()
	if err != nil {
		return nil, err
	}
	return restHealth{client}, nil
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
