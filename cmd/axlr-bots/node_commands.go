package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/underpass-ai/axlr-bots/adapters/unixapi"
)

func nodeList(ctx context.Context, service unixapi.Service, args []string, stdout, stderr io.Writer) int {
	if len(args) != 0 {
		fmt.Fprintln(stderr, "axlr-bots: node list takes no arguments")
		return exitUsage
	}
	nodes, err := service.ListNodes(ctx)
	if err != nil {
		return fail(stderr, err)
	}
	printJSON(stdout, nodes)
	return exitOK
}

func nodeGet(ctx context.Context, service unixapi.Service, args []string, stdout, stderr io.Writer) int {
	if len(args) != 1 {
		fmt.Fprintln(stderr, "axlr-bots: node get needs exactly one <node-id>")
		return exitUsage
	}
	node, err := service.GetNode(ctx, args[0])
	if err != nil {
		return fail(stderr, err)
	}
	printJSON(stdout, node)
	return exitOK
}

// nodeEnrollToken mints a one-time token for a node on another host.
// The token is printed once and stored nowhere; the operator carries it
// (and the control plane's URL) to the new host.
func nodeEnrollToken(ctx context.Context, service unixapi.Service, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("node enroll-token", flag.ContinueOnError)
	flags.SetOutput(stderr)
	nodeID := flags.String("node-id", "", "the id the node's certificate will carry (default: generated)")
	labels := flags.String("labels", "", "labels the node starts with, key=value,key=value")
	ttl := flags.Duration("ttl", 0, "how long the token lives (default 1h)")
	if err := flags.Parse(args); err != nil {
		return exitUsage
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(stderr, "axlr-bots: node enroll-token takes flags only")
		return exitUsage
	}
	parsed, err := parseLabels(*labels)
	if err != nil {
		fmt.Fprintln(stderr, "axlr-bots:", err)
		return exitUsage
	}
	request := unixapi.EnrollTokenRequest{NodeID: *nodeID, Labels: parsed}
	if *ttl != 0 {
		request.TTL = ttl.String()
	}
	token, err := service.CreateEnrollmentToken(ctx, request)
	if err != nil {
		return fail(stderr, err)
	}
	printJSON(stdout, token)
	return exitOK
}

// nodeRevoke cuts a node off: its certificate is refused from the next
// request on and what it was running is failed.
func nodeRevoke(ctx context.Context, service unixapi.Service, args []string, stdout, stderr io.Writer) int {
	if len(args) != 1 {
		fmt.Fprintln(stderr, "axlr-bots: node revoke needs exactly one <node-id>")
		return exitUsage
	}
	result, err := service.RevokeNode(ctx, args[0])
	if err != nil {
		return fail(stderr, err)
	}
	printJSON(stdout, result)
	return exitOK
}

// parseLabels reads key=value,key=value.
func parseLabels(raw string) (map[string]string, error) {
	labels := map[string]string{}
	for _, pair := range strings.Split(raw, ",") {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}
		key, value, ok := strings.Cut(pair, "=")
		if !ok || strings.TrimSpace(key) == "" || strings.TrimSpace(value) == "" {
			return nil, fmt.Errorf("label %q is not key=value", pair)
		}
		labels[strings.TrimSpace(key)] = strings.TrimSpace(value)
	}
	return labels, nil
}
