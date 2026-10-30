package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
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
)

// operatorURL is the base of the links an approval notification carries:
// --api-url, or the operator address itself; a wildcard address names
// the host instead.
func operatorURL(explicit, apiListen string) string {
	if explicit != "" {
		return strings.TrimRight(explicit, "/")
	}
	if apiListen == "" {
		return ""
	}
	host, port, err := net.SplitHostPort(apiListen)
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
	return "https://" + net.JoinHostPort(host, port)
}

// serve runs the control plane as a service until SIGTERM or SIGINT.
// With --listen it also serves the node protocol over TLS, with a
// certificate the fleet CA (ca init) issues for the host's names and
// client certificates verified against that CA; with --api-listen it
// serves the operator routes over TLS on another address, behind the
// bearer token in --api-token-file (generated when the file is missing),
// and --api-url is the base the approval notifications link to.
func serve(ctx context.Context, app application.Dependencies, stateDir, socketPath string, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("serve", flag.ContinueOnError)
	flags.SetOutput(stderr)
	listen := flags.String("listen", os.Getenv("AXLR_BOTS_LISTEN"), "a TLS address to serve nodes on too, such as 127.0.0.1:7878 or 0.0.0.0:7878 (also AXLR_BOTS_LISTEN); needs ca init")
	names := flags.String("server-names", os.Getenv("AXLR_BOTS_SERVER_NAMES"), "names and addresses nodes dial the control plane by, comma-separated, added to the host's own (also AXLR_BOTS_SERVER_NAMES)")
	apiListen := flags.String("api-listen", os.Getenv("AXLR_BOTS_API_LISTEN"), "a TLS address to serve operators and webhook senders on, such as 127.0.0.1:7879 (also AXLR_BOTS_API_LISTEN); needs ca init")
	apiTokenFile := flags.String("api-token-file", "", "the file with the operator bearer token (default <state>/api.token; generated when missing)")
	apiURL := flags.String("api-url", os.Getenv("AXLR_BOTS_API_URL"), "the operator API's base URL that approval notifications link to (also AXLR_BOTS_API_URL; default https://<api-listen>)")
	tick := flags.Duration("tick", daemon.DefaultTick, "how often to look for Pending tasks")
	reconcileEvery := flags.Duration("reconcile-every", daemon.DefaultReconcileEvery, "how often to fail the tasks a dead run left Running")
	grace := flags.Duration("grace", 0, "reconciliation grace (default: the bot's task timeout plus 1m)")
	bus := flags.String("bus", busDefault(os.Getenv("AXLR_BOTS_BUS")), "how pushes reach the nodes: direct (each node's stream over its own connection, always on) or nats (a broker beside it, for nodes that read it; also AXLR_BOTS_BUS)")
	natsURL := flags.String("nats-url", os.Getenv("AXLR_BOTS_NATS_URL"), "with --bus nats, the broker, nats://host:4222 (also AXLR_BOTS_NATS_URL)")
	natsCreds := flags.String("nats-creds", os.Getenv("AXLR_BOTS_NATS_CREDS"), "with --bus nats, a NATS credentials file (also AXLR_BOTS_NATS_CREDS)")
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
	options := daemon.Options{SocketPath: socketPath, Tick: *tick, ReconcileEvery: *reconcileEvery, Grace: *grace}
	if *listen != "" || *apiListen != "" {
		authority, err := pki.Load(caDir(stateDir))
		if err != nil {
			fmt.Fprintln(stderr, "axlr-bots: serve --listen/--api-listen:", err)
			return exitError
		}
		// The names are read at each handshake: a listener on every
		// interface must also carry an address that arrives after start.
		listenAddr, apiAddr, extra := *listen, *apiListen, *names
		hosts := func() []string {
			if listenAddr == "" {
				return serverNames(apiAddr, extra)
			}
			names := serverNames(listenAddr, extra)
			if apiAddr != "" {
				names = append(names, serverNames(apiAddr, "")...)
			}
			return names
		}
		options.ListenAddr = *listen
		options.APIListenAddr = *apiListen
		options.TLS = pki.ServerTLSConfigFor(authority, hosts)
	}
	ctx, stop := signal.NotifyContext(ctx, syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	options.Log = log.New(stderr, "", log.LstdFlags)
	if *bus == "nats" {
		publisher, err := natsbus.NewPublisher(natsbus.Config{URL: *natsURL, Credentials: *natsCreds, Outbox: filepath.Join(stateDir, "outbox", "nats"), Name: "axlr-bots", Log: options.Log})
		if err != nil {
			fmt.Fprintln(stderr, "axlr-bots: serve --bus nats:", err)
			return exitError
		}
		defer publisher.Close()
		options.Pushers = append(options.Pushers, publisher)
		options.Log.Printf("daemon: bus nats at %s (outbox %s)", *natsURL, filepath.Join(stateDir, "outbox", "nats"))
	}
	if *apiListen != "" {
		path := *apiTokenFile
		if path == "" {
			path = filepath.Join(stateDir, apiTokenFileName)
		}
		token, created, err := loadOrCreateAPIToken(path)
		if err != nil {
			fmt.Fprintln(stderr, "axlr-bots: serve --api-listen: token file:", err)
			return exitError
		}
		options.APIToken = token
		if created {
			options.Log.Printf("daemon: operator token written to %s (mode 0600)", path)
		} else {
			options.Log.Printf("daemon: operator token read from %s", path)
		}
	}
	app.OperatorURL = operatorURL(*apiURL, *apiListen)
	if app.OperatorURL != "" {
		options.Log.Printf("daemon: approval notifications link to %s", app.OperatorURL)
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
