package nats

import (
	"context"
	"fmt"
	"log"
	"path/filepath"
	"strings"
	"testing"
	"time"

	natsio "github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/underpass-ai/axlr-bots/adapters/bus/topicwire"
	"github.com/underpass-ai/axlr-bots/adapters/pki"
)

// topicsControlPlanePublishes is what the control plane's user may
// publish for a team's topics, as the ADR and
// docs/architecture/bus-port.md list it: the topics, the JetStream API
// (its streams and its consumer) and the acks of its consumer.
var topicsControlPlanePublishes = []string{topicwire.TopicPrefix + ".>", "$JS.API.>", "$JS.ACK." + topicwire.PublishStream + ".>"}

// appUser is an application's entry: it publishes on a team and reads the
// topics with its own consumer, nothing else. The ADR's list wanted one
// more: CONSUMER.* covers one token, and MSG.NEXT is two
// (docs/architecture/bus-port.md lists the whole entry).
func appUser(name string) string {
	return fmt.Sprintf(`{ user: "CN=%s,O=%s", permissions: {
    publish: ["%s.>", "$JS.API.CONSUMER.*.%s.%[1]s", "$JS.API.CONSUMER.MSG.NEXT.%[4]s.%[1]s", "$JS.ACK.%[4]s.%[1]s.>"],
    subscribe: ["_INBOX.>"] } }`, name, pki.ClientOrganization, topicwire.PublishPrefix, topicwire.TopicStream)
}

// app issues an application's client certificate and answers its
// JetStream client.
func (f fleet) app(t *testing.T, url, name string) jetstream.JetStream {
	t.Helper()
	keyPEM, certPEM, err := f.authority.IssueClient(name, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	cfg := Config{URL: url, CertFile: f.write(t, name+".crt", certPEM), KeyFile: f.write(t, name+".key", keyPEM), CAFile: filepath.Join(f.dir, "ca", "ca.crt"), Name: name}
	opts, err := options(cfg)
	if err != nil {
		t.Fatal(err)
	}
	conn, err := natsio.Connect(url, opts...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(conn.Close)
	js, err := jetstream.New(conn)
	if err != nil {
		t.Fatal(err)
	}
	return js
}

// Over mutual TLS with the ADR's permissions alone, the control plane
// defines both streams, publishes a team's topics and reads and acks the
// publications; an application reads the topics with its own consumer
// and publishes on a team, and may not publish a topic itself.
func TestTopicsOverMutualTLSWithTheADRsPermissions(t *testing.T) {
	f := newFleet(t)
	url := tlsURL(tlsBrokerOf(t, f, []string{controlPlaneUser(topicsControlPlanePublishes), appUser("app")}))
	cfg := f.controlPlane(t, url)
	var logs syncLog
	cfg.Log = log.New(&logs, "", 0)
	topics := topicsOn(t, cfg)
	svc := &service{groups: teams()}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := topics.Listen(ctx, svc); err != nil {
		t.Fatal(err)
	}
	js := f.app(t, url, "app")
	reader := topicReader(t, js, "app")

	// The application may not publish a topic: the broker refuses it.
	forged := natsio.NewMsg(topicwire.TopicSubject("default", "team-a", "order.placed"))
	forged.Data = []byte(`{}`)
	short, stop := context.WithTimeout(ctx, 2*time.Second)
	if _, err := js.PublishMsg(short, forged); err == nil {
		t.Error("the application published a topic")
	}
	stop()

	if err := topics.Tap(&recording{}).Publish(ctx, botEvent("evt-1", "", "team-a")); err != nil {
		t.Fatal(err)
	}
	messages := readTopics(t, reader, 1)
	if !strings.Contains(string(messages[0].Data()), `"id":"evt-1"`) {
		t.Errorf("message = %s", messages[0].Data())
	}

	msg, err := topicwire.EncodePublication("default", "team-a", "k-1", topicwire.PublicationDTO{Name: "order.placed", Message: "m"})
	if err != nil {
		t.Fatal(err)
	}
	publishOn(t, js, msg.Subject, msg.MsgID, string(msg.Payload))
	waitUntil(t, "the publication", func() bool { n, _ := svc.counts(); return n == 1 })
	// Acked: the control plane's ack permission is enough, or the
	// publication would wait for redelivery after its ack wait.
	opts, err := options(cfg)
	if err != nil {
		t.Fatal(err)
	}
	conn, err := natsio.Connect(url, opts...)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	cp, err := jetstream.New(conn)
	if err != nil {
		t.Fatal(err)
	}
	consumer, err := cp.Consumer(ctx, topicwire.PublishStream, topicwire.ControlPlaneConsumer)
	if err != nil {
		t.Fatal(err)
	}
	waitUntil(t, "the ack", func() bool {
		info, err := consumer.Info(ctx)
		return err == nil && info.NumAckPending == 0 && info.Delivered.Consumer == 1
	})
	if strings.Contains(logs.String(), "not acked") {
		t.Errorf("logs = %q", logs.String())
	}
	if n, _ := svc.counts(); n != 1 {
		t.Errorf("published %d times", n)
	}
}
