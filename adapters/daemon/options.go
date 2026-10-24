package daemon

import (
	"io"
	"log"
	"time"
)

// Defaults of the daemon's loop.
const (
	DefaultTick           = 2 * time.Second
	DefaultReconcileEvery = time.Minute
)

// Options tune the daemon.
type Options struct {
	// SocketPath is where the local API listens.
	SocketPath string
	// ListenAddr is an optional TCP address for nodes on other hosts
	// (127.0.0.1:7878 on one host); empty listens on the socket only.
	ListenAddr string
	// Tick is how often the worker looks for Pending tasks.
	Tick time.Duration
	// ReconcileEvery is how often tasks a dead run left Running are failed.
	ReconcileEvery time.Duration
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

func (o Options) log() *log.Logger {
	if o.Log != nil {
		return o.Log
	}
	return log.New(io.Discard, "", 0)
}
