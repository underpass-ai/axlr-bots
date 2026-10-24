package main

import (
	"context"
	"fmt"
	"io"

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
