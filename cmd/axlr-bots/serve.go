package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	natsbus "github.com/underpass-ai/axlr-bots/adapters/bus/nats"
	"github.com/underpass-ai/axlr-bots/adapters/daemon"
	"github.com/underpass-ai/axlr-bots/adapters/pki"
	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
	"github.com/underpass-ai/axlr-bots/internal/statelock"
)

// operatorAddress is the address the actions of an approval notification
// name: --grpc-url, or the gRPC operator address itself; a wildcard
// address names the host instead.
func operatorAddress(explicit, grpcListen string) string {
	if explicit != "" {
		return strings.TrimRight(explicit, "/")
	}
	if grpcListen == "" {
		return ""
	}
	host, port, err := net.SplitHostPort(grpcListen)
	if err != nil {
		return ""
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		if name, err := os.Hostname(); err == nil && name != "" {
			host = name
		} else {
			host = "localhost"
		}
	}
	return net.JoinHostPort(host, port)
}

// serve runs the control plane as a service until SIGTERM or SIGINT.
// With --listen it also serves the node protocol over TLS, with a
// certificate the fleet CA (ca init) issues for the host's names and
// client certificates verified against that CA; with --api-listen it
// serves the webhook triggers over TLS on another address (each trigger
// authenticates with its own secret or signing key), and with
// --grpc-listen the operator API over gRPC behind an operator
// certificate and the bearer token in --api-token-file (generated when
// the file is missing); --grpc-url is the address the approval
// notifications name for deciding.
func serve(ctx context.Context, app application.Dependencies, stateDir, socketPath string, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("serve", flag.ContinueOnError)
	flags.SetOutput(stderr)
	listen := flags.String("listen", os.Getenv("AXLR_BOTS_LISTEN"), "a TLS address to serve nodes on too, such as 127.0.0.1:7878 or 0.0.0.0:7878 (also AXLR_BOTS_LISTEN); needs ca init")
	names := flags.String("server-names", os.Getenv("AXLR_BOTS_SERVER_NAMES"), "names and addresses nodes dial the control plane by, comma-separated, added to the host's own (also AXLR_BOTS_SERVER_NAMES)")
	apiListen := flags.String("api-listen", os.Getenv("AXLR_BOTS_API_LISTEN"), "a TLS address to serve webhook triggers and /healthz on, such as 127.0.0.1:7879 (also AXLR_BOTS_API_LISTEN); needs ca init; it carries no operator API, which is gRPC only")
	grpcListen := flags.String("grpc-listen", os.Getenv("AXLR_BOTS_GRPC_LISTEN"), "a TLS address to serve the operator API over gRPC on, such as 127.0.0.1:7880 (also AXLR_BOTS_GRPC_LISTEN); needs ca init, takes the bearer token in --api-token-file and an operator client certificate (ca operator issue); the same API is always on the gRPC socket")
	grpcSocket := flags.String("grpc-socket", os.Getenv("AXLR_BOTS_GRPC_SOCKET"), "the Unix socket to serve the operator API over gRPC on, with no token (also AXLR_BOTS_GRPC_SOCKET; default <state>/axlr-bots-grpc.sock)")
	metricsListen := flags.String("metrics-listen", os.Getenv("AXLR_BOTS_METRICS_LISTEN"), "a plain-HTTP address to serve Prometheus on, GET /metrics and GET /healthz alone, such as 0.0.0.0:9464 (also AXLR_BOTS_METRICS_LISTEN; empty: none); counts, no secret: keep it inside the cluster")
	otlpEndpoint := flags.String("otlp-endpoint", os.Getenv("AXLR_BOTS_OTLP_ENDPOINT"), "an OTLP/HTTP endpoint to send one trace per ended task to, such as http://tempo:4318 (also AXLR_BOTS_OTLP_ENDPOINT; empty: nothing is exported)")
	logFormat := logFormatFlag(flags)
	apiTokenFile := flags.String("api-token-file", "", "the file with the operator bearer token (default <state>/api.token; generated when missing)")
	grpcURL := flags.String("grpc-url", os.Getenv("AXLR_BOTS_GRPC_URL"), "the operator gRPC address, host:port, that approval notifications name for approving or denying (also AXLR_BOTS_GRPC_URL; default --grpc-listen)")
	minAXLR := flags.String("min-axlr-version", os.Getenv("AXLR_BOTS_MIN_AXLR_VERSION"), "the least AXLR the nodes should run, a semantic version such as 0.20.0, answered by the GetBuild call so a client flags an older node (also AXLR_BOTS_MIN_AXLR_VERSION; empty: no minimum)")
	tick := flags.Duration("tick", daemon.DefaultTick, "how often to look for Pending tasks")
	reconcileEvery := flags.Duration("reconcile-every", daemon.DefaultReconcileEvery, "how often to fail the tasks a dead run left Running")
	grace := flags.Duration("grace", 0, "reconciliation grace (default: the bot's task timeout plus 1m)")
	bus := flags.String("bus", busDefault(os.Getenv("AXLR_BOTS_BUS")), "how pushes reach the nodes: direct (each node's stream over its own connection, always on) or nats (a broker beside it, for nodes that read it; also AXLR_BOTS_BUS)")
	natsURL := flags.String("nats-url", os.Getenv("AXLR_BOTS_NATS_URL"), "with --bus nats, the broker, nats://host:4222 (also AXLR_BOTS_NATS_URL)")
	natsCreds := flags.String("nats-creds", os.Getenv("AXLR_BOTS_NATS_CREDS"), "with --bus nats, a NATS credentials file (also AXLR_BOTS_NATS_CREDS)")
	natsCert := flags.String("nats-cert", os.Getenv("AXLR_BOTS_NATS_CERT"), "with --bus nats, a client certificate in PEM for mutual TLS, such as one ca issue-client wrote (also AXLR_BOTS_NATS_CERT)")
	natsKey := flags.String("nats-key", os.Getenv("AXLR_BOTS_NATS_KEY"), "with --nats-cert, its key (also AXLR_BOTS_NATS_KEY)")
	natsCA := flags.String("nats-ca", os.Getenv("AXLR_BOTS_NATS_CA"), "with --bus nats, the CA the broker's certificate is checked against (also AXLR_BOTS_NATS_CA; default the system's)")
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
	if *bus != "direct" && *bus != "nats" {
		fmt.Fprintln(stderr, "axlr-bots: --bus is direct or nats")
		return exitUsage
	}
	if *bus == "nats" && *natsURL == "" {
		fmt.Fprintln(stderr, "axlr-bots: --bus nats needs --nats-url (or AXLR_BOTS_NATS_URL)")
		return exitUsage
	}
	if (*natsCert == "") != (*natsKey == "") {
		fmt.Fprintln(stderr, "axlr-bots: --nats-cert and --nats-key go together")
		return exitUsage
	}
	if *minAXLR != "" {
		minimum, err := domain.ParseSemanticVersion("--min-axlr-version", *minAXLR)
		if err != nil {
			fmt.Fprintln(stderr, "axlr-bots: serve:", err)
			return exitUsage
		}
		app.MinimumAXLR = minimum
	}
	app = withModelProxy(app, stderr)
	grpcSocketPath := *grpcSocket
	if grpcSocketPath == "" {
		grpcSocketPath = filepath.Join(stateDir, "axlr-bots-grpc.sock")
	}
	options := daemon.Options{StateDir: stateDir, SocketPath: socketPath, GRPCSocketPath: grpcSocketPath, Tick: *tick, ReconcileEvery: *reconcileEvery, Grace: *grace}
	if *listen != "" || *apiListen != "" || *grpcListen != "" {
		authority, err := pki.Load(caDir(stateDir))
		if err != nil {
			fmt.Fprintln(stderr, "axlr-bots: serve --listen/--api-listen/--grpc-listen:", err)
			return exitError
		}
		// The names are read at each handshake: a listener on every
		// interface must also carry an address that arrives after start.
		listenAddr, apiAddr, grpcAddr, extra := *listen, *apiListen, *grpcListen, *names
		hosts := func() []string {
			var names []string
			rest := extra
			for _, addr := range []string{listenAddr, apiAddr, grpcAddr} {
				if addr == "" {
					continue
				}
				names = append(names, serverNames(addr, rest)...)
				rest = ""
			}
			return names
		}
		options.ListenAddr = *listen
		options.APIListenAddr = *apiListen
		options.GRPCListenAddr = *grpcListen
		options.TLS = pki.ServerTLSConfigFor(authority, hosts)
	}
	// A second serve on the state is refused here, before its bus sweeps
	// the first one's outbox; daemon.Run takes the lock for good.
	probe, err := statelock.Acquire(stateDir)
	if err != nil {
		return fail(stderr, err)
	}
	probe()
	ctx, stop := signal.NotifyContext(ctx, syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	loggers, err := newLoggers(stderr, *logFormat)
	if err != nil {
		return fail(stderr, err)
	}
	options.Log, options.ErrorLog = loggers.Info, loggers.Error
	options.MetricsListenAddr = *metricsListen
	options.OTLPEndpoint = *otlpEndpoint
	if *bus == "nats" {
		publisher, err := natsbus.NewPublisher(natsbus.Config{URL: *natsURL, Credentials: *natsCreds, CertFile: *natsCert, KeyFile: *natsKey, CAFile: *natsCA, Outbox: filepath.Join(stateDir, "outbox", "nats"), Name: "axlr-bots", Log: options.Log})
		if err != nil {
			fmt.Fprintln(stderr, "axlr-bots: serve --bus nats:", err)
			return exitError
		}
		defer publisher.Close()
		options.Pushers = append(options.Pushers, publisher)
		options.Log.Printf("daemon: bus nats at %s (outbox %s)", *natsURL, filepath.Join(stateDir, "outbox", "nats"))
	}
	if *grpcListen != "" {
		path := *apiTokenFile
		if path == "" {
			path = filepath.Join(stateDir, apiTokenFileName)
		}
		token, created, err := loadOrCreateAPIToken(path)
		if err != nil {
			fmt.Fprintln(stderr, "axlr-bots: serve --grpc-listen: token file:", err)
			return exitError
		}
		options.APIToken = token
		if created {
			options.Log.Printf("daemon: operator token written to %s (mode 0600)", path)
		} else {
			options.Log.Printf("daemon: operator token read from %s", path)
		}
	}
	app.OperatorAddress = operatorAddress(*grpcURL, *grpcListen)
	app.Daemon = true
	if app.OperatorAddress != "" {
		options.Log.Printf("daemon: approval notifications name the operator address %s", app.OperatorAddress)
	}
	if !app.MinimumAXLR.IsZero() {
		options.Log.Printf("daemon: nodes should run AXLR %s or newer", app.MinimumAXLR)
	}
	if err := daemon.Run(ctx, app, options); err != nil {
		fmt.Fprintln(stderr, "axlr-bots:", err)
		return exitError
	}
	return exitOK
}

// busDefault is the bus when the environment names none: direct.
func busDefault(env string) string {
	if env == "" {
		return "direct"
	}
	return env
}
