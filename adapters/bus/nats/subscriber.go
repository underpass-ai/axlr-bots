package nats

import (
	"context"
	"time"

	"github.com/nats-io/nats.go/jetstream"

	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

// Subscriber is the node's side: nodeagent.Streamer over the broker. A
// node reads its own subject with a durable consumer named after it, so
// a push made while it was away is read when it comes back; each push
// is acknowledged once the agent took it.
type Subscriber struct {
	cfg Config
}

// NewSubscriber reads the node's pushes from the broker.
func NewSubscriber(cfg Config) *Subscriber { return &Subscriber{cfg: cfg} }

// Stream opens the node's consumer and answers its pushes as they come,
// a resync first; the channel closes when ctx ends or the broker is lost
// (the agent opens it again). When ctx ends it closes once the broker
// has let go of the stream.
func (s *Subscriber) Stream(ctx context.Context, node domain.NodeID) (<-chan application.NodePush, error) {
	conn, js, err := connect(s.cfg, false)
	if err != nil {
		return nil, err
	}
	setup, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	consumer, err := js.CreateOrUpdateConsumer(setup, s.cfg.stream(), jetstream.ConsumerConfig{
		Durable: consumerName(node), FilterSubject: subject(node), AckPolicy: jetstream.AckExplicitPolicy, DeliverPolicy: jetstream.DeliverNewPolicy,
		AckWait: 30 * time.Second, MaxAckPending: 256, InactiveThreshold: 24 * time.Hour,
	})
	if err != nil {
		conn.Close()
		return nil, err
	}
	messages, err := consumer.Messages()
	if err != nil {
		conn.Close()
		return nil, err
	}
	logger := s.cfg.log()
	logger.Printf("bus: nats: node %s reads %s on %s", node, subject(node), conn.ConnectedUrl())
	pushes := make(chan application.NodePush, 64)
	go func() {
		<-ctx.Done()
		// Drained, Next ends only once the broker answered the
		// unsubscribe, so when the channel closes the broker holds no pull
		// of this stream and a push made after waits for the node's next
		// one. Stopped, the channel could close first: the broker handed
		// such a push to a reader that was gone, and it came back only
		// after AckWait. A broker that is away has nothing to be told.
		if conn.IsConnected() {
			messages.Drain()
		} else {
			messages.Stop()
		}
	}()
	go func() {
		defer close(pushes)
		defer conn.Close()
		pushes <- application.NodePush{All: true}
		for {
			msg, err := messages.Next()
			if err != nil {
				if ctx.Err() == nil {
					logger.Printf("bus: nats: node %s: %v", node, err)
				}
				return
			}
			push, ok := decode(msg.Data())
			if !ok {
				_ = msg.Ack()
				continue
			}
			push.Ack = func() { _ = msg.Ack() }
			select {
			case pushes <- push:
			case <-ctx.Done():
				// The stream is ending: the broker hands the push to the
				// node's next stream at once, and the drain goes on.
				_ = msg.Nak()
			}
		}
	}()
	return pushes, nil
}
