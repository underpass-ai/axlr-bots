package nats

import (
	"context"
	"errors"
	"fmt"
	"time"

	natsio "github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/underpass-ai/axlr-bots/adapters/bus/topicwire"
	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

const (
	// publicationAckWait is how long the broker waits for the control
	// plane to publish one before it gives it again.
	publicationAckWait = time.Minute
	// maxRedeliveryDelay bounds the wait before a publication that failed
	// is given again.
	maxRedeliveryDelay = time.Minute
)

// Listen reads the publications with the control plane's durable
// consumer, from the first the stream keeps that it has not acked, and
// publishes each on its team as the operator, one at a time, until ctx
// ends. One refused (a bad envelope, a missing key, an unknown team, the
// rules of the publication) is taken off for good (Term), logged and
// audited; one that failed (the store) is given again after a growing
// delay; one published is acked. It returns at once: it connects when
// the broker answers, and again whenever it is lost.
func (t *Topics) Listen(ctx context.Context, service PublicationService) error {
	if service == nil {
		return errors.New("nats: the publications need a service")
	}
	go func() {
		for ctx.Err() == nil {
			conn, messages, err := t.consume(ctx)
			if err != nil {
				t.cfg.log().Printf("bus: nats: publications: %v (retrying)", err)
				select {
				case <-ctx.Done():
					return
				case <-time.After(2 * time.Second):
				}
				continue
			}
			t.read(ctx, conn, messages, service)
		}
	}()
	return nil
}

// consume connects, defines the streams and the consumer, and opens its
// messages.
func (t *Topics) consume(ctx context.Context) (*natsio.Conn, jetstream.MessagesContext, error) {
	conn, js, err := connectDefining(t.cfg, publishStream(t.cfg), topicStream(t.cfg))
	if err != nil {
		return nil, nil, err
	}
	setup, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	consumer, err := js.CreateOrUpdateConsumer(setup, topicwire.PublishStream, jetstream.ConsumerConfig{
		Durable: topicwire.ControlPlaneConsumer, FilterSubject: topicwire.PublishPrefix + ".>", AckPolicy: jetstream.AckExplicitPolicy,
		DeliverPolicy: jetstream.DeliverAllPolicy, AckWait: publicationAckWait, MaxAckPending: 256,
	})
	if err != nil {
		conn.Close()
		return nil, nil, err
	}
	messages, err := consumer.Messages()
	if err != nil {
		conn.Close()
		return nil, nil, err
	}
	t.cfg.log().Printf("bus: nats: the control plane reads %s.> on %s", topicwire.PublishPrefix, conn.ConnectedUrl())
	return conn, messages, nil
}

// read handles the consumer's messages until ctx ends or the broker is
// lost.
func (t *Topics) read(ctx context.Context, conn *natsio.Conn, messages jetstream.MessagesContext, service PublicationService) {
	defer conn.Close()
	stopped := make(chan struct{})
	defer close(stopped)
	go func() {
		select {
		case <-ctx.Done():
			messages.Stop()
		case <-stopped:
		}
	}()
	for {
		msg, err := messages.Next()
		if err != nil {
			if ctx.Err() == nil {
				t.cfg.log().Printf("bus: nats: publications: %v", err)
			}
			messages.Stop()
			return
		}
		t.handle(ctx, msg, service)
	}
}

// handle publishes one publication, or refuses it, or gives it back.
func (t *Topics) handle(ctx context.Context, msg jetstream.Msg, service PublicationService) {
	key := msg.Headers().Get(topicwire.MsgIDHeader)
	publication, err := topicwire.DecodePublication(msg.Subject(), msg.Data(), key)
	if err != nil {
		address, _ := topicwire.ParsePublishSubject(msg.Subject())
		t.refuse(ctx, msg, service, application.RefusedPublication{Tenant: address.TenantToken, Team: address.TeamToken, Name: address.Name, Key: key, Reason: err.Error()})
		return
	}
	refusal := application.RefusedPublication{Tenant: publication.Address.TenantToken, Team: publication.Address.TeamToken, Name: publication.DTO.Name, Key: publication.Key}
	groups, err := service.ListGroups(ctx)
	if err != nil {
		t.retry(msg, publication.Key, err)
		return
	}
	team, tenant, err := resolve(groups, publication.Address)
	if err != nil {
		refusal.Reason = err.Error()
		t.refuse(ctx, msg, service, refusal)
		return
	}
	refusal.Team, refusal.Tenant = team, tenant
	answer, err := service.PublishGroupEvent(ctx, topicwire.PublicationCommand(publication.DTO, team, tenant, publication.Key))
	if err != nil {
		if refusedForGood(err) {
			refusal.Reason = err.Error()
			t.refuse(ctx, msg, service, refusal)
			return
		}
		t.retry(msg, publication.Key, err)
		return
	}
	if answer.Created {
		t.cfg.log().Printf("bus: nats: publication %s on %s/%s published as %s (%d deliveries)", publication.Key, tenant, team, answer.EventID, len(answer.Deliveries))
	} else {
		t.cfg.log().Printf("bus: nats: publication %s on %s/%s repeats %s: nothing delivered again", publication.Key, tenant, team, answer.EventID)
	}
	t.ack(ctx, msg)
}

// resolve finds the one team the subject's tokens name.
func resolve(groups []application.Group, address topicwire.Address) (string, string, error) {
	var found []application.Group
	for _, group := range groups {
		if topicwire.Token(group.TenantID) == address.TenantToken && topicwire.Token(group.Name) == address.TeamToken {
			found = append(found, group)
		}
	}
	switch len(found) {
	case 0:
		return "", "", domain.NotFoundError{Kind: "group", ID: address.TenantToken + "/" + address.TeamToken}
	case 1:
		return found[0].Name, found[0].TenantID, nil
	}
	return "", "", fmt.Errorf("topics: %d teams share the subject's tenant %s and team %s; rename one", len(found), address.TenantToken, address.TeamToken)
}

// refusedForGood reports an error of the publication itself, which no
// retry mends: the rules (a reserved kind, a member that is none, the
// data), an unknown team, a state or a conflict. Anything else (the
// store, a timeout) is worth another try.
func refusedForGood(err error) bool {
	var validation domain.ValidationError
	var notFound domain.NotFoundError
	var forbidden domain.ForbiddenError
	var conflict domain.ConflictError
	var state domain.StateError
	return errors.As(err, &validation) || errors.As(err, &notFound) || errors.As(err, &forbidden) || errors.As(err, &conflict) || errors.As(err, &state)
}

// refuse audits the publication refused and takes it off for good; one
// whose audit fails is given again, so nothing is turned away unseen.
func (t *Topics) refuse(ctx context.Context, msg jetstream.Msg, service PublicationService, refusal application.RefusedPublication) {
	if err := service.RefusePublication(ctx, refusal); err != nil {
		t.retry(msg, refusal.Key, fmt.Errorf("auditing its refusal: %w", err))
		return
	}
	t.cfg.log().Printf("bus: nats: publication %q on %s refused and taken off: %s", refusal.Key, msg.Subject(), refusal.Reason)
	if err := msg.Term(); err != nil {
		t.cfg.log().Printf("bus: nats: publication on %s not taken off: %v", msg.Subject(), err)
	}
}

// retry gives the publication back after a growing delay.
func (t *Topics) retry(msg jetstream.Msg, key string, err error) {
	delay := redeliveryDelay(msg)
	t.cfg.log().Printf("bus: nats: publication %q on %s not published: %v (again in %s)", key, msg.Subject(), err, delay)
	_ = msg.NakWithDelay(delay)
}

// ack acks the publication and waits for the broker to confirm it: an
// ack the broker never took is logged, and the publication comes again
// after the ack wait, a repeat its key makes nothing of.
func (t *Topics) ack(ctx context.Context, msg jetstream.Msg) {
	confirm, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := msg.DoubleAck(confirm); err != nil {
		t.cfg.log().Printf("bus: nats: publication on %s not acked: %v", msg.Subject(), err)
	}
}

// redeliveryDelay is how long a failed publication waits before it is
// given again: a quarter second, doubled for each delivery, at most a
// minute.
func redeliveryDelay(msg jetstream.Msg) time.Duration {
	delivered := uint64(1)
	if meta, err := msg.Metadata(); err == nil && meta.NumDelivered > 0 {
		delivered = meta.NumDelivered
	}
	delay := 250 * time.Millisecond
	for i := uint64(1); i < delivered && delay < maxRedeliveryDelay; i++ {
		delay *= 2
	}
	return min(delay, maxRedeliveryDelay)
}
