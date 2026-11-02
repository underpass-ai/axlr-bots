package nats

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	natsio "github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/underpass-ai/axlr-bots/adapters/bus/bustest"
	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

// broker starts a NATS server with JetStream for one test.
func broker(t *testing.T) *server.Server {
	t.Helper()
	return brokerWith(t, nil)
}

// brokerWith is broker with a change to the server's options.
func brokerWith(t *testing.T, change func(*server.Options)) *server.Server {
	t.Helper()
	opts := &server.Options{Host: "127.0.0.1", Port: -1, JetStream: true, StoreDir: t.TempDir(), NoLog: true, NoSigs: true}
	if change != nil {
		change(opts)
	}
	s, err := server.NewServer(opts)
	if err != nil {
		t.Fatal(err)
	}
	go s.Start()
	if !s.ReadyForConnections(10 * time.Second) {
		t.Fatal("the broker did not start")
	}
	t.Cleanup(func() { s.Shutdown(); s.WaitForShutdown() })
	return s
}

func factoryOn(url string) bustest.Factory {
	return func(t *testing.T) (application.NodePusher, bustest.Open) {
		cfg := Config{URL: url, Outbox: filepath.Join(t.TempDir(), "outbox"), Name: "test", Log: log.New(os.Stderr, "", 0)}
		publisher, err := NewPublisher(cfg)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(publisher.Close)
		return publisher, NewSubscriber(cfg).Stream
	}
}

func TestNATSMeetsTheBusContract(t *testing.T) {
	bustest.Run(t, factoryOn(broker(t).ClientURL()))
}

// A node that was away reads the pushes made meanwhile: its consumer is
// durable. A push made while the broker is down waits in the outbox and
// is published when the broker is back.
func TestPushesSurviveTheNodesAndTheBrokersAbsence(t *testing.T) {
	s := broker(t)
	url := s.ClientURL()
	port := s.Addr().(*net.TCPAddr).Port
	pusher, open := factoryOn(url)(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	first, stop := context.WithCancel(ctx)
	stream, err := open(first, bustest.NodeA)
	if err != nil {
		t.Fatal(err)
	}
	<-stream // the resync
	stop()
	for range stream {
	}
	// The node is away: its stream closed, and the broker holds no pull
	// of it. The push is on the broker once it left the outbox.
	publisher := pusher.(*Publisher)
	if err := pusher.Push(ctx, bustest.NodeA, application.NodePush{Execution: "exec-away"}); err != nil {
		t.Fatal(err)
	}
	for deadline := time.Now().Add(10 * time.Second); len(publisher.box.sweep()) > 0; time.Sleep(10 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("the push made while the node was away was not published")
		}
	}
	stream, err = open(ctx, bustest.NodeA)
	if err != nil {
		t.Fatal(err)
	}
	got := map[domain.ExecutionID]bool{}
	deadline := time.After(5 * time.Second)
	for len(got) == 0 {
		select {
		case push := <-stream:
			if push.Ack != nil {
				push.Ack()
			}
			if !push.All {
				got[push.Execution] = true
			}
		case <-deadline:
			t.Fatal("the push made while the node was away never came")
		}
	}
	if !got["exec-away"] {
		t.Errorf("got %v", got)
	}

	// The broker goes away: the push waits in the outbox.
	s.Shutdown()
	s.WaitForShutdown()
	if err := pusher.Push(ctx, bustest.NodeA, application.NodePush{Execution: "exec-later"}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(500 * time.Millisecond)
	if left := publisher.box.sweep(); len(left) != 1 || !strings.Contains(string(left[0].payload), "exec-later") {
		t.Fatalf("outbox = %+v", left)
	}
	publisher.Close()
	// A new publisher over the same outbox publishes what was left once a
	// broker answers at the same address.
	again, err := server.NewServer(&server.Options{Host: "127.0.0.1", Port: port, JetStream: true, StoreDir: t.TempDir(), NoLog: true, NoSigs: true})
	if err != nil {
		t.Fatal(err)
	}
	go again.Start()
	if !again.ReadyForConnections(10 * time.Second) {
		t.Fatal("the second broker did not start")
	}
	defer func() { again.Shutdown(); again.WaitForShutdown() }()
	restarted, err := NewPublisher(publisher.cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	for deadline := time.Now().Add(10 * time.Second); len(restarted.box.sweep()) > 0; time.Sleep(50 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("the outbox was not drained")
		}
	}
}

func TestSubjectsAndWire(t *testing.T) {
	if subject("rack.worker-1") != "axlr.push.rack_worker-1" || consumerName("web 1") != "node-web_1" {
		t.Error("subject tokens")
	}
	push, ok := decode(encode(application.NodePush{Execution: "exec-1"}))
	if !ok || push.Execution != "exec-1" || push.All {
		t.Errorf("execution = %+v %v", push, ok)
	}
	if push, ok := decode(encode(application.NodePush{All: true})); !ok || !push.All {
		t.Errorf("resync = %+v %v", push, ok)
	}
	for _, bad := range []string{"nope", `{"type":"ping"}`, `{"type":"execution","executionId":"Bad Id"}`} {
		if _, ok := decode([]byte(bad)); ok {
			t.Errorf("%s decoded", bad)
		}
	}
	if _, err := NewPublisher(Config{}); err == nil {
		t.Error("a publisher without a URL")
	}
	if _, err := NewPublisher(Config{URL: "nats://127.0.0.1:1"}); err == nil {
		t.Error("a publisher without an outbox")
	}
}

// publisherOn starts a publisher on the broker with its own outbox.
func publisherOn(t *testing.T, url string) *Publisher {
	t.Helper()
	publisher, err := NewPublisher(Config{URL: url, Outbox: filepath.Join(t.TempDir(), "outbox"), Name: "test", Log: log.New(os.Stderr, "", 0)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(publisher.Close)
	return publisher
}

// drained waits for the publisher's outbox to empty.
func drained(t *testing.T, publisher *Publisher, within time.Duration) {
	t.Helper()
	for deadline := time.Now().Add(within); len(publisher.box.sweep()) > 0; time.Sleep(20 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("the outbox still holds %d pushes", len(publisher.box.sweep()))
		}
	}
}

// operatorJS is a JetStream client of the test's own, as an operator's.
func operatorJS(t *testing.T, url string) jetstream.JetStream {
	t.Helper()
	conn, err := natsio.Connect(url)
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

// streamMessages is how many pushes the stream holds.
func streamMessages(t *testing.T, js jetstream.JetStream) uint64 {
	t.Helper()
	stream, err := js.Stream(context.Background(), DefaultStream)
	if err != nil {
		t.Fatal(err)
	}
	info, err := stream.Info(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return info.State.Msgs
}

// A stream an operator deletes while the control plane runs is defined
// again, and the push lands on it instead of being retried for ever.
func TestADeletedStreamIsDefinedAgain(t *testing.T) {
	url := broker(t).ClientURL()
	publisher := publisherOn(t, url)
	ctx := context.Background()
	if err := publisher.Push(ctx, bustest.NodeA, application.NodePush{Execution: "exec-1"}); err != nil {
		t.Fatal(err)
	}
	drained(t, publisher, 10*time.Second)
	js := operatorJS(t, url)
	if err := js.DeleteStream(ctx, DefaultStream); err != nil {
		t.Fatal(err)
	}
	if err := publisher.Push(ctx, bustest.NodeA, application.NodePush{Execution: "exec-2"}); err != nil {
		t.Fatal(err)
	}
	drained(t, publisher, 15*time.Second)
	if got := streamMessages(t, js); got != 1 {
		t.Errorf("the stream defined again holds %d pushes, want 1", got)
	}
}

// writeOld puts in the outbox a push made age ago, as a control plane
// stopped that long would have left it.
func writeOld(t *testing.T, outboxDir string, node domain.NodeID, age time.Duration) string {
	t.Helper()
	dir := filepath.Join(outboxDir, token(node))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, fmt.Sprintf("%020d-%012d.json", time.Now().Add(-age).UnixNano(), 1))
	raw := `{"node": "` + node.String() + `", "payload": ` + string(encode(application.NodePush{Execution: "exec-old"})) + `}`
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// A push older than the stream keeps one is dropped from the outbox at
// start, unpublished: the node resyncs when it comes back.
func TestAnOutboxPushOlderThanTheRetentionIsDropped(t *testing.T) {
	url := broker(t).ClientURL()
	outboxDir := filepath.Join(t.TempDir(), "outbox")
	old := writeOld(t, outboxDir, bustest.NodeA, 2*time.Hour)
	recent := writeOld(t, outboxDir, bustest.NodeB, time.Minute)
	var logs syncLog
	publisher, err := NewPublisher(Config{URL: url, Outbox: outboxDir, Name: "test", Log: log.New(&logs, "", 0)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(publisher.Close)
	if _, err := os.Stat(old); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the 2-h-old push is still in the outbox: %v", err)
	}
	drained(t, publisher, 10*time.Second)
	if _, err := os.Stat(recent); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the recent push was not published: %v", err)
	}
	if got := streamMessages(t, operatorJS(t, url)); got != 1 {
		t.Errorf("the stream holds %d pushes, want only the recent one", got)
	}
	if !strings.Contains(logs.String(), "dropped 1 pushes older than") {
		t.Errorf("logs = %q", logs.String())
	}
	// One that aged while it waited in the queue is dropped there.
	e := entry{id: fmt.Sprintf("%020d-%012d", time.Now().Add(-2*time.Hour).UnixNano(), 9), node: bustest.NodeA, payload: encode(application.NodePush{Execution: "exec-stale"})}
	if !e.olderThan(Retention, time.Now()) || (entry{id: "not-a-time"}).olderThan(Retention, time.Now()) {
		t.Error("olderThan")
	}
	publisher.enqueue(e)
	for deadline := time.Now().Add(5 * time.Second); !strings.Contains(logs.String(), e.id); time.Sleep(20 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("the stale queued push was not dropped: %q", logs.String())
		}
	}
	if got := streamMessages(t, operatorJS(t, url)); got != 1 {
		t.Errorf("the stream holds %d pushes after the stale one", got)
	}
}

// A push the broker refuses for good is dropped after a few tries, and
// the one queued behind it goes out.
func TestARefusedPushDoesNotHoldBackTheNext(t *testing.T) {
	url := brokerWith(t, func(o *server.Options) { o.MaxPayload = 2048 }).ClientURL()
	publisher := publisherOn(t, url)
	big := []byte(`"` + strings.Repeat("x", 4096) + `"`)
	bad, err := publisher.box.add(bustest.NodeA, publisher.seq.Add(1), big)
	if err != nil {
		t.Fatal(err)
	}
	publisher.enqueue(bad)
	if err := publisher.Push(context.Background(), bustest.NodeA, application.NodePush{Execution: "exec-good"}); err != nil {
		t.Fatal(err)
	}
	drained(t, publisher, 20*time.Second)
	if got := streamMessages(t, operatorJS(t, url)); got != 1 {
		t.Errorf("the stream holds %d pushes, want the good one", got)
	}
}

func TestRefusalsAreTheBrokersForGood(t *testing.T) {
	for _, tc := range []struct {
		err              error
		refused, suspect bool
	}{
		{&jetstream.APIError{Code: 400, ErrorCode: 10054}, true, true},
		{fmt.Errorf("wrapped: %w", &jetstream.APIError{Code: 404}), true, true},
		{&jetstream.APIError{Code: 408}, false, false},
		{&jetstream.APIError{Code: 503}, false, false},
		{jetstream.ErrNoStreamResponse, false, true},
		{natsio.ErrMaxPayload, true, false},
		{context.DeadlineExceeded, false, false},
	} {
		if refused(tc.err) != tc.refused || streamSuspect(tc.err) != tc.suspect {
			t.Errorf("%v: refused %v, suspect %v", tc.err, refused(tc.err), streamSuspect(tc.err))
		}
	}
}

// syncLog is a log the publisher's goroutine writes while the test
// reads it.
type syncLog struct {
	mu  sync.Mutex
	buf strings.Builder
}

func (l *syncLog) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.Write(p)
}

func (l *syncLog) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.String()
}
