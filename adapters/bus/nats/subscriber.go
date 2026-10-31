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
// (the agent opens it again).
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
		messages.Stop()
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
				return
			}
		}
	}()
	return pushes, nil
}
