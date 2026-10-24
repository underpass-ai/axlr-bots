package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/underpass-ai/axlr-bots/adapters/axlr"
	"github.com/underpass-ai/axlr-bots/adapters/nodeagent"
	"github.com/underpass-ai/axlr-bots/adapters/unixapi"
)

// Exit codes of the command.
const (
	exitOK    = 0
	exitError = 1
	exitUsage = 2
)

// run parses the flags and serves until SIGTERM or SIGINT.
func run(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("axlr-node", flag.ContinueOnError)
	flags.SetOutput(stderr)
	control := flags.String("control", envOr("AXLR_NODE_CONTROL", ""), "the control plane: unix:///path/to/axlr-bots.sock or http://host:port (also AXLR_NODE_CONTROL)")
	nodeID := flags.String("node-id", envOr("AXLR_NODE_ID", defaultNodeID()), "this node's id (also AXLR_NODE_ID; default: the host name)")
	labels := flags.String("labels", envOr("AXLR_NODE_LABELS", ""), "labels for placement selectors, key=value,key=value (also AXLR_NODE_LABELS)")
	stateDir := flags.String("state", envOr("AXLR_NODE_STATE", defaultStateDir()), "the node's state: traces, policies and workspaces of the executions it runs")
	axlrBin := flags.String("axlr-bin", envOr("AXLR_TUI", "axlr-tui"), "the axlr-tui that runs executions (also AXLR_TUI)")
	heartbeat := flags.Duration("heartbeat", nodeagent.DefaultHeartbeat, "how often to tell the control plane the node is alive")
	poll := flags.Duration("poll", nodeagent.DefaultPollWait, "how long one poll for an assignment waits")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return exitOK
		}
		return exitUsage
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(stderr, "axlr-node: takes flags only")
		return exitUsage
	}
	if *control == "" {
		fmt.Fprintln(stderr, "axlr-node: --control (or AXLR_NODE_CONTROL) names the control plane")
		return exitUsage
	}
	parsedLabels, err := parseLabels(*labels)
	if err != nil {
		fmt.Fprintln(stderr, "axlr-node:", err)
		return exitUsage
	}
	if err := os.MkdirAll(*stateDir, 0o755); err != nil {
		fmt.Fprintln(stderr, "axlr-node: state directory:", err)
		return exitError
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	agent := nodeagent.Agent{
		Transport: unixapi.NewClient(*control),
		Executor:  axlr.NewExecutor(*axlrBin, *stateDir),
		Options:   nodeagent.Options{NodeID: *nodeID, Labels: parsedLabels, Heartbeat: *heartbeat, PollWait: *poll, Log: log.New(stderr, "", log.LstdFlags)},
	}
	if err := agent.Run(ctx); err != nil {
		fmt.Fprintln(stderr, "axlr-node:", err)
		return exitError
	}
	_ = stdout
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
		if !ok || key == "" || value == "" {
			return nil, fmt.Errorf("label %q is not key=value", pair)
		}
		labels[strings.TrimSpace(key)] = strings.TrimSpace(value)
	}
	return labels, nil
}

func defaultNodeID() string {
	host, err := os.Hostname()
	if err != nil || host == "" {
		return "node"
	}
	return strings.ToLower(strings.SplitN(host, ".", 2)[0])
}

func defaultStateDir() string {
	if dir := os.Getenv("XDG_STATE_HOME"); dir != "" {
		return filepath.Join(dir, "axlr-node")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".", "axlr-node-state")
	}
	return filepath.Join(home, ".local", "state", "axlr-node")
}

func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
