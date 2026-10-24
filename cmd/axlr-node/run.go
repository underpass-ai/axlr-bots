package main

import (
	"context"
	"encoding/json"
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
	"time"

	"github.com/underpass-ai/axlr-bots/adapters/axlr"
	"github.com/underpass-ai/axlr-bots/adapters/nodeagent"
	"github.com/underpass-ai/axlr-bots/adapters/pki"
	"github.com/underpass-ai/axlr-bots/adapters/unixapi"
	"github.com/underpass-ai/axlr-bots/application"
)

// Exit codes of the command.
const (
	exitOK    = 0
	exitError = 1
	exitUsage = 2
)

// run parses the flags and serves until SIGTERM or SIGINT; `enroll`
// and `check` are the subcommands.
func run(args []string, stdout, stderr io.Writer) int {
	if len(args) > 0 && args[0] == "enroll" {
		return enroll(args[1:], stdout, stderr)
	}
	if len(args) > 0 && args[0] == "check" {
		return check(args[1:], stdout, stderr)
	}
	flags := flag.NewFlagSet("axlr-node", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.Usage = func() {
		fmt.Fprintln(stderr, "usage: axlr-node [flags]            run the node agent")
		fmt.Fprintln(stderr, "       axlr-node enroll [flags]     enroll this host with a one-time token")
		fmt.Fprintln(stderr, "       axlr-node check [flags]      check the control plane answers over TLS with the fleet CA")
		flags.PrintDefaults()
	}
	control := flags.String("control", envOr("AXLR_NODE_CONTROL", ""), "the control plane: unix:///path/to/axlr-bots.sock on the same host, or https://host:7878 with the credentials enroll stored (also AXLR_NODE_CONTROL)")
	nodeID := flags.String("node-id", envOr("AXLR_NODE_ID", ""), "this node's id over the socket (also AXLR_NODE_ID; default: the host name); over https it is the certificate's")
	labels := flags.String("labels", envOr("AXLR_NODE_LABELS", ""), "labels for placement selectors, key=value,key=value (also AXLR_NODE_LABELS)")
	stateDir := flags.String("state", envOr("AXLR_NODE_STATE", defaultStateDir()), "the node's state: traces, policies and workspaces of the executions it runs")
	axlrBin := flags.String("axlr-bin", envOr("AXLR_TUI", "axlr-tui"), "the axlr-tui that runs executions (also AXLR_TUI)")
	heartbeat := flags.Duration("heartbeat", nodeagent.DefaultHeartbeat, "how often to tell the control plane the node is alive")
	poll := flags.Duration("poll", nodeagent.DefaultPollWait, "how long one poll for an assignment waits")
	tokenFile := flags.String("token-file", envOr("AXLR_NODE_TOKEN_FILE", ""), "over https, enroll with the token in this file when the state holds no certificate, then run (also AXLR_NODE_TOKEN_FILE): a sidecar's first start")
	caFile := flags.String("ca-file", envOr("AXLR_NODE_CA_FILE", ""), "with --token-file, the fleet CA in PEM instead of the one the token carries (also AXLR_NODE_CA_FILE)")
	retry := flags.Duration("enroll-retry", 10*time.Second, "with --token-file, how long to wait between enrollment attempts")
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
	if *tokenFile != "" && !strings.HasPrefix(*control, "https://") {
		fmt.Fprintln(stderr, "axlr-node: --token-file needs --control https://host:port (the control plane's TLS address)")
		return exitUsage
	}
	if err := os.MkdirAll(*stateDir, 0o755); err != nil {
		fmt.Fprintln(stderr, "axlr-node: state directory:", err)
		return exitError
	}
	logger := log.New(stderr, "", log.LstdFlags)
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	if *tokenFile != "" {
		start := firstStart{Control: *control, TokenFile: *tokenFile, CAFile: *caFile, StateDir: *stateDir, NodeID: *nodeID}
		if !start.untilEnrolled(ctx, *retry, logger.Printf) {
			return exitOK
		}
	}
	transport, id, labelsFromEnrollment, identity, err := transportFor(*control, *nodeID, *stateDir)
	if err != nil {
		fmt.Fprintln(stderr, "axlr-node:", err)
		return exitError
	}
	for key, value := range parsedLabels {
		labelsFromEnrollment[key] = value
	}
	logger.Printf("node: control plane %s, %s", *control, identity)
	agent := nodeagent.Agent{
		Transport: transport,
		Executor:  axlr.NewExecutor(*axlrBin, *stateDir),
		Options:   nodeagent.Options{NodeID: id, Labels: labelsFromEnrollment, Heartbeat: *heartbeat, PollWait: *poll, Log: logger},
	}
	if err := agent.Run(ctx); err != nil {
		fmt.Fprintln(stderr, "axlr-node:", err)
		return exitError
	}
	_ = stdout
	return exitOK
}

// printJSON writes v as indented JSON.
func printJSON(w io.Writer, v any) {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		fmt.Fprintln(w, "{}")
		return
	}
	fmt.Fprintln(w, string(data))
}

// transportFor picks how the agent reaches the control plane. Over
// https the identity is the certificate enroll stored: its common name
// is the node id, and a --node-id that disagrees is refused; the labels
// the enrollment gave the node come first (flags add to them). Over the
// socket the id is the flag's, or the host name. The last value says
// which, for the log.
func transportFor(control, nodeID, stateDir string) (application.NodeTransport, string, map[string]string, string, error) {
	labels := map[string]string{}
	if !strings.HasPrefix(control, "https://") {
		if nodeID == "" {
			nodeID = defaultNodeID()
		}
		return unixapi.NewClient(control), nodeID, labels, "node id " + nodeID + " from the host (no certificate over the socket)", nil
	}
	creds, err := pki.LoadCredentials(stateDir)
	if err != nil {
		return nil, "", nil, "", err
	}
	if nodeID != "" && nodeID != creds.NodeID {
		return nil, "", nil, "", fmt.Errorf("--node-id %q disagrees with the certificate, which names %q", nodeID, creds.NodeID)
	}
	config, err := creds.ClientTLSConfig()
	if err != nil {
		return nil, "", nil, "", err
	}
	record, err := loadRecord(stateDir)
	if err != nil {
		return nil, "", nil, "", err
	}
	for key, value := range record.Labels {
		labels[key] = value
	}
	identity := fmt.Sprintf("client certificate %s for node %s (serial %s, until %s)", filepath.Join(stateDir, pki.CredentialsDirName, "node.crt"), creds.NodeID, creds.Serial, creds.NotAfter.UTC().Format("2006-01-02"))
	return unixapi.NewTLSClient(control, config), creds.NodeID, labels, identity, nil
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
