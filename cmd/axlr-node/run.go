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
	natsbus "github.com/underpass-ai/axlr-bots/adapters/bus/nats"
	"github.com/underpass-ai/axlr-bots/adapters/jev"
	"github.com/underpass-ai/axlr-bots/adapters/nodeagent"
	"github.com/underpass-ai/axlr-bots/adapters/pki"
	"github.com/underpass-ai/axlr-bots/adapters/unixapi"
	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

// Exit codes of the command.
const (
	exitOK    = 0
	exitError = 1
	exitUsage = 2
)

// run parses the flags and serves until SIGTERM or SIGINT; `enroll`,
// `check` and `mcp-relay` are the subcommands.
func run(args []string, stdout, stderr io.Writer) int {
	if len(args) > 0 && args[0] == "enroll" {
		return enroll(args[1:], stdout, stderr)
	}
	if len(args) > 0 && args[0] == "check" {
		return check(args[1:], stdout, stderr)
	}
	if len(args) > 0 && args[0] == "mcp-relay" {
		return mcpRelay(args[1:], stdinOf, stdout, stderr)
	}
	flags := flag.NewFlagSet("axlr-node", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.Usage = func() {
		fmt.Fprintln(stderr, "usage: axlr-node [flags]            run the node agent")
		fmt.Fprintln(stderr, "       axlr-node enroll [flags]     enroll this host with a one-time token")
		fmt.Fprintln(stderr, "       axlr-node check [flags]      check the control plane answers over TLS with the fleet CA")
		fmt.Fprintln(stderr, "       axlr-node mcp-relay --socket <path>   the delegation tool's stdio server, which AXLR starts per execution")
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
	dedicated := flags.Bool("dedicated", envOr("AXLR_NODE_DEDICATED", "") == "true", "run only the bots whose placement names this node or selects its labels, never those placed on any node (also AXLR_NODE_DEDICATED=true): a sidecar")
	axlrMCPConfig := flags.String("axlr-mcp-config", envOr("AXLR_NODE_AXLR_MCP_CONFIG", ""), "the mcp.json every AXLR execution loads instead of the operator's ~/.config/axlr/mcp.json, so a server the operator added does not join the bots' runs (also AXLR_NODE_AXLR_MCP_CONFIG; an absolute path that exists)")
	mcpConfig := flags.String("mcp-config", envOr("AXLR_NODE_MCP_CONFIG", ""), "the mcp.json a Jev bot's program finds its MCP server in (also AXLR_NODE_MCP_CONFIG; default: ~/.config/axlr/mcp.json, the node's AXLR's)")
	jevModel := flags.String("jev-model", envOr("AXLR_NODE_JEV_MODEL", jev.DefaultModel), "the pinned Jev version the jev runtime asks (also AXLR_NODE_JEV_MODEL)")
	bus := flags.String("bus", envOr("AXLR_NODE_BUS", "direct"), "how the control plane's pushes reach this node: direct (a stream over the node's own connection) or nats (also AXLR_NODE_BUS)")
	natsURL := flags.String("nats-url", envOr("AXLR_NODE_NATS_URL", ""), "with --bus nats, the broker, nats://host:4222 (also AXLR_NODE_NATS_URL)")
	natsCreds := flags.String("nats-creds", envOr("AXLR_NODE_NATS_CREDS", ""), "with --bus nats, a NATS credentials file (also AXLR_NODE_NATS_CREDS)")
	natsCert := flags.String("nats-cert", envOr("AXLR_NODE_NATS_CERT", ""), "with --bus nats, a client certificate in PEM for mutual TLS (also AXLR_NODE_NATS_CERT; default with a tls:// broker and an https control plane: the node's own, from enrollment)")
	natsKey := flags.String("nats-key", envOr("AXLR_NODE_NATS_KEY", ""), "with --nats-cert, its key (also AXLR_NODE_NATS_KEY)")
	natsCA := flags.String("nats-ca", envOr("AXLR_NODE_NATS_CA", ""), "with --bus nats, the CA the broker's certificate is checked against (also AXLR_NODE_NATS_CA; default the system's)")
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
	if *bus != "direct" && *bus != "nats" {
		fmt.Fprintln(stderr, "axlr-node: --bus is direct or nats")
		return exitUsage
	}
	if *bus == "nats" && *natsURL == "" {
		fmt.Fprintln(stderr, "axlr-node: --bus nats needs --nats-url (or AXLR_NODE_NATS_URL)")
		return exitUsage
	}
	if (*natsCert == "") != (*natsKey == "") {
		fmt.Fprintln(stderr, "axlr-node: --nats-cert and --nats-key go together")
		return exitUsage
	}
	if *tokenFile != "" && !strings.HasPrefix(*control, "https://") {
		fmt.Fprintln(stderr, "axlr-node: --token-file needs --control https://host:port (the control plane's TLS address)")
		return exitUsage
	}
	if *axlrMCPConfig != "" {
		if _, err := os.Stat(*axlrMCPConfig); err != nil || !filepath.IsAbs(*axlrMCPConfig) {
			fmt.Fprintln(stderr, "axlr-node: --axlr-mcp-config needs an absolute path to an mcp.json that exists:", *axlrMCPConfig)
			return exitUsage
		}
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
	executor := axlr.NewExecutor(*axlrBin, *stateDir)
	executor.MCPConfig = *axlrMCPConfig
	// AXLR reaches the delegation tool through this binary's mcp-relay.
	if self, err := os.Executable(); err == nil {
		executor.Relay = self
	} else {
		logger.Printf("node: own path unknown (%v): no delegation tool", err)
	}
	runtimes := nodeagent.NewRuntimes(executor)
	// The jev runtime is there when the host has Jev's key: a Jev bot is
	// never assigned to a node that cannot ask Jev.
	if key := os.Getenv("TYPESAFE_API_KEY"); key != "" {
		judge, err := jev.NewClient(jev.ClientConfig{APIKey: key, Model: *jevModel})
		if err != nil {
			fmt.Fprintln(stderr, "axlr-node:", err)
			return exitUsage
		}
		runtimes.Add(domain.RuntimeJev, jev.NewExecutor(judge, jev.MCPConfig{Path: *mcpConfig}, *stateDir))
		logger.Printf("node: jev runtime on (%s)", *jevModel)
	} else {
		logger.Printf("node: TYPESAFE_API_KEY is not set: Jev bots are not run here")
	}
	agent := nodeagent.Agent{
		Transport: transport,
		Executor:  runtimes,
		Options:   nodeagent.Options{NodeID: id, Labels: labelsFromEnrollment, Dedicated: *dedicated, Runtimes: runtimes.Kinds(), Heartbeat: *heartbeat, PollWait: *poll, Log: logger, SocketDir: filepath.Join(*stateDir, "run")},
	}
	if *bus == "nats" {
		cert, key, how := natsClientCertificate(*natsURL, *control, *stateDir, *natsCert, *natsKey)
		agent.Streamer = natsbus.NewSubscriber(natsbus.Config{URL: *natsURL, Credentials: *natsCreds, CertFile: cert, KeyFile: key, CAFile: *natsCA, Name: "axlr-node " + id, Log: logger})
		logger.Printf("node: bus nats at %s, %s", *natsURL, how)
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

// natsClientCertificate is the certificate the node shows the broker:
// the one named by the flags, or, with a tls:// broker and an https
// control plane, the node's own from enrollment, so a broker that maps
// certificates to users knows the node by the same identity the control
// plane does. It says which, for the log.
func natsClientCertificate(natsURL, control, stateDir, cert, key string) (string, string, string) {
	if cert != "" {
		return cert, key, "client certificate " + cert
	}
	if strings.HasPrefix(natsURL, "tls://") && strings.HasPrefix(control, "https://") {
		dir := filepath.Join(stateDir, pki.CredentialsDirName)
		return filepath.Join(dir, "node.crt"), filepath.Join(dir, "node.key"), "the node's own certificate from enrollment"
	}
	return "", "", "no client certificate"
}
