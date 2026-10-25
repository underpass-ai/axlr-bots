package daemon

import (
	"crypto/tls"
	"io"
	"log"
	"net"
	"time"
)

// Defaults of the daemon's loop.
const (
	DefaultTick           = 2 * time.Second
	DefaultReconcileEvery = time.Minute
	DefaultDeliverEvery   = 2 * time.Second
)

// Options tune the daemon.
type Options struct {
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
	// APIListenAddr is an optional TLS address for operators and webhook
	// senders (127.0.0.1:7879): the operator routes alone, behind
	// APIToken; empty serves operators on the socket only. TLS must be
	// set with it.
	APIListenAddr string
	// APIListener is an already bound listener for the operator address,
	// in place of APIListenAddr.
	APIListener net.Listener
	// APIToken is the bearer token the operator address requires.
	APIToken string
	// Tick is how often the worker looks for Pending tasks.
	Tick time.Duration
	// ReconcileEvery is how often tasks a dead run left Running are failed.
	ReconcileEvery time.Duration
	// DeliverEvery is how often the notifications that are due go out.
	DeliverEvery time.Duration
	// Grace is the reconciliation grace; zero means the bot's task
	// timeout plus a minute, as `task reconcile` does.
	Grace time.Duration
	// NodeOfflineAfter is how long without a heartbeat a node is marked
	// Offline; zero means application.NodeOfflineAfter.
	NodeOfflineAfter time.Duration
	// Log receives one line per task run, reconciliation and error;
	// nil discards them.
	Log *log.Logger
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

func (o Options) deliverEvery() time.Duration {
	if o.DeliverEvery > 0 {
		return o.DeliverEvery
	}
	return DefaultDeliverEvery
}

func (o Options) log() *log.Logger {
	if o.Log != nil {
		return o.Log
	}
	return log.New(io.Discard, "", 0)
}
