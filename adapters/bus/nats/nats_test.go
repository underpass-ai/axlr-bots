package nats

import (
	"context"
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"

	"github.com/underpass-ai/axlr-bots/adapters/bus/bustest"
	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

// broker starts a NATS server with JetStream for one test.
func broker(t *testing.T) *server.Server {
	t.Helper()
	s, err := server.NewServer(&server.Options{Host: "127.0.0.1", Port: -1, JetStream: true, StoreDir: t.TempDir(), NoLog: true, NoSigs: true})
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
	if err := pusher.Push(ctx, bustest.NodeA, application.NodePush{Execution: "exec-away"}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond)
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
	publisher := pusher.(*Publisher)
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
