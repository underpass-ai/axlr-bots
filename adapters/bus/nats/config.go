// Package nats is the bus adapter over a NATS broker with JetStream: the
// control plane publishes each push to a node on the node's own subject
// through an outbox, and the node reads its subject with a durable
// consumer, so a push survives the broker's and the node's absences and
// a node that was away reads what it missed. Nothing here is known to
// the domain or the use cases: they see application.NodePusher and
// nodeagent.Streamer.
package nats

import (
	"encoding/json"
	"log"
	"strings"
	"time"

	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
	"github.com/underpass-ai/axlr-bots/nodeproto"
)

// DefaultStream is the JetStream stream that keeps the pushes; its
// subjects are SubjectPrefix.<node>.
const (
	DefaultStream = "AXLR_PUSH"
	SubjectPrefix = "axlr.push"
	// Retention bounds what the broker keeps: a push older than this is
	// of no use, the node resyncs when it comes back.
	Retention = time.Hour
	// Duplicates is the window the broker drops a republished push in.
	Duplicates = 2 * time.Minute
)

// Config is how either side reaches the broker.
type Config struct {
	// URL is the broker's, nats://host:4222; several comma-separated.
	URL string
	// Credentials is a NATS credentials file (user JWT and seed); empty
	// connects without one.
	Credentials string
	// CertFile and KeyFile are a client certificate and its key in PEM,
	// for mutual TLS: a broker with verify_and_map takes the user from
	// the certificate's subject. Both or neither.
	CertFile string
	KeyFile  string
	// CAFile is the PEM bundle the broker's certificate is checked
	// against; empty uses the system's roots on a TLS connection.
	CAFile string
	// Stream is the JetStream stream; empty is DefaultStream.
	Stream string
	// Outbox is the directory the publisher keeps a push in until the
	// broker took it; the publisher needs it.
	Outbox string
	// Name is how the connection is known to the broker.
	Name string
	// Log hears connections, retries and refusals; nil discards.
	Log *log.Logger
}

func (c Config) stream() string {
	if c.Stream != "" {
		return c.Stream
	}
	return DefaultStream
}

func (c Config) log() *log.Logger {
	if c.Log != nil {
		return c.Log
	}
	return log.New(discard{}, "", 0)
}

type discard struct{}

func (discard) Write(p []byte) (int, error) { return len(p), nil }

// subject is the node's subject: the id's dots become underscores, a
// dot being a subject separator.
func subject(node domain.NodeID) string {
	return SubjectPrefix + "." + token(node)
}

// consumerName is the node's durable consumer.
func consumerName(node domain.NodeID) string { return "node-" + token(node) }

func token(node domain.NodeID) string {
	return strings.NewReplacer(".", "_", "*", "_", ">", "_", " ", "_").Replace(node.String())
}

// encode is a push on the wire: the stream message of the node protocol.
func encode(push application.NodePush) []byte {
	message := nodeproto.StreamMessage{Type: nodeproto.StreamExecution, ExecutionID: push.Execution.String()}
	if push.All {
		message = nodeproto.StreamMessage{Type: nodeproto.StreamResync}
	}
	raw, _ := json.Marshal(message)
	return raw
}

// decode reads a push off the wire; false for a message that is none.
func decode(raw []byte) (application.NodePush, bool) {
	var message nodeproto.StreamMessage
	if json.Unmarshal(raw, &message) != nil {
		return application.NodePush{}, false
	}
	switch message.Type {
	case nodeproto.StreamResync:
		return application.NodePush{All: true}, true
	case nodeproto.StreamExecution:
		execution, err := domain.NewExecutionID(message.ExecutionID)
		if err != nil {
			return application.NodePush{}, false
		}
		return application.NodePush{Execution: execution}, true
	}
	return application.NodePush{}, false
}
