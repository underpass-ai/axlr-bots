package nodeagent

import (
	"io"
	"log"
	"time"
)

// maxSocketPath is the longest Unix socket path the agent opens: the
// kernel's sun_path is 108 bytes on Linux, 104 on the BSDs.
const maxSocketPath = 104

// Defaults of the agent's loop.
const (
	DefaultHeartbeat = 15 * time.Second
	DefaultPollWait  = 20 * time.Second
	DefaultRenew     = 10 * time.Second
	DefaultUpload    = 2 * time.Minute
)

// Options tune the agent.
type Options struct {
	// NodeID is how the control plane knows this host.
	NodeID string
	// HostName is reported at registration; empty means the OS's.
	HostName string
	// Labels are the node's labels, for placement selectors.
	Labels map[string]string
	// Dedicated registers a node that runs only the bots placed on it by
	// name or selector, never those placed on any node.
	Dedicated bool
	// Runtimes names the runtimes the node has beside AXLR (jev): the
	// control plane assigns it a bot of another runtime only when named.
	Runtimes []string
	// Heartbeat is how often the agent reports it is alive.
	Heartbeat time.Duration
	// PollWait is how long one long poll for an assignment waits.
	PollWait time.Duration
	// Renew is how often a running execution's lease is renewed.
	Renew time.Duration
	// Upload bounds the upload of one artifact.
	Upload time.Duration
	// SocketDir holds the socket the agent serves the delegation tool on
	// for each execution of a bot that may delegate (mode 0700); empty
	// serves no tool.
	SocketDir string
	// Log receives one line per registration, assignment and report;
	// nil discards them.
	Log *log.Logger
}

func (o Options) heartbeat() time.Duration {
	if o.Heartbeat > 0 {
		return o.Heartbeat
	}
	return DefaultHeartbeat
}

func (o Options) pollWait() time.Duration {
	if o.PollWait > 0 {
		return o.PollWait
	}
	return DefaultPollWait
}

func (o Options) renew() time.Duration {
	if o.Renew > 0 {
		return o.Renew
	}
	return DefaultRenew
}

func (o Options) uploadTimeout() time.Duration {
	if o.Upload > 0 {
		return o.Upload
	}
	return DefaultUpload
}

func (o Options) log() *log.Logger {
	if o.Log != nil {
		return o.Log
	}
	return log.New(io.Discard, "", 0)
}
