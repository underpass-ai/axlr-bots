package daemon

import (
	"crypto/tls"
	"io"
	"log"
	"net"
	"path/filepath"
	"time"

	"github.com/underpass-ai/axlr-bots/adapters/tracing"
	"github.com/underpass-ai/axlr-bots/application"
)

// Defaults of the daemon's loop.
const (
	DefaultTick            = 2 * time.Second
	DefaultReconcileEvery  = time.Minute
	DefaultDeliverEvery    = 2 * time.Second
	DefaultBuildCheckEvery = 30 * time.Second
)

// Options tune the daemon.
type Options struct {
	// StateDir is the state directory the dependencies work on. Run
	// holds its lock (statelock) for its whole life, so a second daemon
	// or a command that would write the files directly is refused while
	// this one runs. Required.
	StateDir string
	// SocketPath is where the local API listens.
	SocketPath string
	// ListenAddr is an optional TLS address for nodes on other hosts
	// (127.0.0.1:7878 on one host, 0.0.0.0:7878 for a fleet); empty
	// listens on the socket only. TLS must be set with it.
	ListenAddr string
	// Listener is an already bound listener for the TLS address, in
	// place of ListenAddr (a test that asked for port 0).
	Listener net.Listener
	// TLS is the configuration of the TLS address: the server
	// certificate issued by the fleet CA and client certificates verified
	// against it (pki.ServerTLSConfig).
	TLS *tls.Config
	// APIListenAddr is an optional TLS address for webhook senders
	// (127.0.0.1:7879): the webhook trigger routes and the health check
	// alone, each trigger authenticating with its own secret or signing
	// key; the operator API is gRPC only (GRPCListenAddr). Empty serves
	// webhooks on the socket only. TLS must be set with it.
	APIListenAddr string
	// APIListener is an already bound listener for the webhook address,
	// in place of APIListenAddr.
	APIListener net.Listener
	// APIToken is the bearer token the gRPC operator address requires.
	APIToken string
	// GRPCListenAddr is an optional TLS address for the operator API over
	// gRPC (127.0.0.1:7880), behind APIToken and a client certificate
	// of an operator (ca operator issue); the gRPC API is also served
	// on GRPCSocketPath whenever the daemon runs. TLS must be set with it.
	GRPCListenAddr string
	// GRPCListener is an already bound listener for the gRPC address, in
	// place of GRPCListenAddr.
	GRPCListener net.Listener
	// GRPCSocketPath is where the gRPC API listens on the host; empty is
	// axlr-bots-grpc.sock beside SocketPath.
	GRPCSocketPath string
	// MetricsListenAddr is an optional plain-HTTP address for Prometheus
	// (0.0.0.0:9464 in a pod): GET /metrics and GET /healthz, nothing
	// else. Counts, no secret; keep it inside the cluster.
	MetricsListenAddr string
	// MetricsListener is an already bound listener for the metrics
	// address, in place of MetricsListenAddr.
	MetricsListener net.Listener
	// OTLPEndpoint, when set, exports one OpenTelemetry trace per ended
	// task to this OTLP/HTTP endpoint (http://tempo:4318); empty creates
	// no exporter and records nothing (docs/architecture/tracing.md).
	OTLPEndpoint string
	// TraceExporter is a span exporter to use in place of the OTLP one
	// (a test's in-memory exporter); setting it turns tracing on.
	TraceExporter tracing.SpanExporter
	// Tick is how often the worker looks for Pending tasks.
	Tick time.Duration
	// ReconcileEvery is how often tasks a dead run left Running are failed.
	ReconcileEvery time.Duration
	// DeliverEvery is how often the notifications that are due go out.
	DeliverEvery time.Duration
	// BuildCheckEvery is how often the installed binary is compared with
	// the running build; a newer one stops the daemon with ErrStaleBuild.
	BuildCheckEvery time.Duration
	// Grace is the reconciliation grace; zero means the bot's task
	// timeout plus a minute, as `task reconcile` does.
	Grace time.Duration
	// NodeOfflineAfter is how long without a heartbeat a node is marked
	// Offline; zero means application.NodeOfflineAfter.
	NodeOfflineAfter time.Duration
	// Pushers are the bus adapters the pushes to nodes also go through
	// (a broker), beside the stream each node holds open to the daemon.
	Pushers []application.NodePusher
	// Log receives one line per task run, reconciliation and error;
	// nil discards them.
	Log application.LogPrinter
	// ErrorLog receives the errors; nil sends them to Log.
	ErrorLog application.LogPrinter
}

func (o Options) tick() time.Duration {
	if o.Tick > 0 {
		return o.Tick
	}
	return DefaultTick
}

func (o Options) reconcileEvery() time.Duration {
	if o.ReconcileEvery > 0 {
		return o.ReconcileEvery
	}
	return DefaultReconcileEvery
}

func (o Options) buildCheckEvery() time.Duration {
	if o.BuildCheckEvery > 0 {
		return o.BuildCheckEvery
	}
	return DefaultBuildCheckEvery
}

func (o Options) deliverEvery() time.Duration {
	if o.DeliverEvery > 0 {
		return o.DeliverEvery
	}
	return DefaultDeliverEvery
}

func (o Options) log() application.LogPrinter {
	if o.Log != nil {
		return o.Log
	}
	return log.New(io.Discard, "", 0)
}

func (o Options) errorLog() application.LogPrinter {
	if o.ErrorLog != nil {
		return o.ErrorLog
	}
	return o.log()
}

// grpcSocketPath is the Unix socket of the gRPC API: the one given, or
// axlr-bots-grpc.sock in the HTTP socket's directory.
func (o Options) grpcSocketPath() string {
	if o.GRPCSocketPath != "" {
		return o.GRPCSocketPath
	}
	return filepath.Join(filepath.Dir(o.SocketPath), "axlr-bots-grpc.sock")
}
