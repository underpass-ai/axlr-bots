package nats

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"

	"github.com/underpass-ai/axlr-bots/adapters/bus/bustest"
	"github.com/underpass-ai/axlr-bots/adapters/pki"
	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

// fleet is a fleet CA with the broker's certificate, the control
// plane's client certificate and one per node, as files.
type fleet struct {
	dir       string
	authority pki.Authority
}

func newFleet(t *testing.T) fleet {
	t.Helper()
	dir := t.TempDir()
	authority, err := pki.Generate(filepath.Join(dir, "ca"))
	if err != nil {
		t.Fatal(err)
	}
	f := fleet{dir: dir, authority: authority}
	server, err := authority.IssueServer([]string{"127.0.0.1", "localhost"}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(server.PrivateKey)
	if err != nil {
		t.Fatal(err)
	}
	f.write(t, "server.key", pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}))
	f.write(t, "server.crt", pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate[0]}))
	keyPEM, certPEM, err := authority.IssueClient("control-plane", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	f.write(t, "control-plane.key", keyPEM)
	f.write(t, "control-plane.crt", certPEM)
	return f
}

func (f fleet) write(t *testing.T, name string, data []byte) string {
	t.Helper()
	path := filepath.Join(f.dir, name)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func (f fleet) path(name string) string { return filepath.Join(f.dir, name) }

// node enrolls a node as the control plane does: the node's own key and
// a certificate the CA signed for its request.
func (f fleet) node(t *testing.T, node domain.NodeID) {
	t.Helper()
	keyPEM, csrPEM, err := pki.NewNodeKeyAndRequest(node)
	if err != nil {
		t.Fatal(err)
	}
	issued, err := f.authority.IssueNode(csrPEM, node, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	f.write(t, string(node)+".key", keyPEM)
	f.write(t, string(node)+".crt", issued.CertificatePEM)
}

// nodeUser is the broker's user for a node: its certificate's subject,
// its own consumer and nothing else (the bus port's rule 6). The same
// list examples/helm/orchestra renders into its nats.conf.
func nodeUser(node domain.NodeID) string {
	consumer := consumerName(node)
	return fmt.Sprintf(`{ user: "CN=%s,O=%s", permissions: {
    publish: ["$JS.API.STREAM.INFO.%[3]s", "$JS.API.CONSUMER.CREATE.%[3]s.%[4]s.%[5]s", "$JS.API.CONSUMER.INFO.%[3]s.%[4]s",
              "$JS.API.CONSUMER.MSG.NEXT.%[3]s.%[4]s", "$JS.ACK.%[3]s.%[4]s.>"],
    subscribe: ["_INBOX.>"] } }`, node, pki.NodeOrganization, DefaultStream, consumer, subject(node))
}

// tlsBroker starts a broker that verifies every client's certificate
// against the fleet CA and takes the user from its subject.
func tlsBroker(t *testing.T, f fleet, nodes ...domain.NodeID) *server.Server {
	t.Helper()
	users := []string{fmt.Sprintf(`{ user: "CN=control-plane,O=%s", permissions: { publish: ["%s.>", "$JS.API.>"], subscribe: ["_INBOX.>"] } }`, pki.ClientOrganization, SubjectPrefix)}
	for _, node := range nodes {
		users = append(users, nodeUser(node))
	}
	conf := fmt.Sprintf(`listen: "127.0.0.1:-1"
jetstream { store_dir: %q }
tls { cert_file: %q, key_file: %q, ca_file: %q, verify_and_map: true, timeout: 2 }
accounts { AXLR { jetstream: enabled, users: [
%s
] } }
no_auth_user: ""
`, filepath.Join(f.dir, "js"), f.path("server.crt"), f.path("server.key"), filepath.Join(f.dir, "ca", "ca.crt"), strings.Join(users, ",\n"))
	opts, err := server.ProcessConfigFile(f.write(t, "nats.conf", []byte(conf)))
	if err != nil {
		t.Fatal(err)
	}
	opts.NoLog, opts.NoSigs = true, true
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

// tlsURL is the broker's address as tls://, whichever scheme it reports.
func tlsURL(s *server.Server) string {
	url := s.ClientURL()
	return "tls://" + url[strings.Index(url, "://")+3:]
}

func (f fleet) controlPlane(t *testing.T, url string) Config {
	return Config{URL: url, CertFile: f.path("control-plane.crt"), KeyFile: f.path("control-plane.key"), CAFile: filepath.Join(f.dir, "ca", "ca.crt"),
		Outbox: filepath.Join(t.TempDir(), "outbox"), Name: "control plane", Log: log.New(os.Stderr, "", 0)}
}

func (f fleet) nodeConfig(url string, node domain.NodeID) Config {
	return Config{URL: url, CertFile: f.path(string(node) + ".crt"), KeyFile: f.path(string(node) + ".key"), CAFile: filepath.Join(f.dir, "ca", "ca.crt"),
		Name: "node " + string(node), Log: log.New(os.Stderr, "", 0)}
}

// The bus contract holds over mutual TLS with the users and permissions
// the orchestra's broker has: the control plane defines the stream and
// publishes, each node reads its own consumer with its own certificate.
func TestNATSOverMutualTLSMeetsTheBusContract(t *testing.T) {
	f := newFleet(t)
	f.node(t, bustest.NodeA)
	f.node(t, bustest.NodeB)
	url := tlsURL(tlsBroker(t, f, bustest.NodeA, bustest.NodeB))
	bustest.Run(t, func(t *testing.T) (application.NodePusher, bustest.Open) {
		publisher, err := NewPublisher(f.controlPlane(t, url))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(publisher.Close)
		return publisher, func(ctx context.Context, node domain.NodeID) (<-chan application.NodePush, error) {
			return NewSubscriber(f.nodeConfig(url, node)).Stream(ctx, node)
		}
	})
}

// A node cannot read another node's pushes, a certificate the broker
// maps to no user is refused, and so is a client without a certificate.
func TestMutualTLSKeepsEachNodeToItsOwnSubject(t *testing.T) {
	f := newFleet(t)
	f.node(t, bustest.NodeA)
	f.node(t, bustest.NodeB)
	f.node(t, "stranger")
	url := tlsURL(tlsBroker(t, f, bustest.NodeA, bustest.NodeB))
	publisher, err := NewPublisher(f.controlPlane(t, url))
	if err != nil {
		t.Fatal(err)
	}
	defer publisher.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	// Node A's certificate asking for node B's consumer.
	if stream, err := NewSubscriber(f.nodeConfig(url, bustest.NodeA)).Stream(ctx, bustest.NodeB); err == nil {
		for range stream {
		}
		t.Error("node A opened node B's consumer")
	}
	// A certificate of the fleet that names no user of the broker.
	if _, err := NewSubscriber(f.nodeConfig(url, "stranger")).Stream(ctx, "stranger"); err == nil {
		t.Error("a node the broker does not list connected")
	}
	// No certificate at all.
	anonymous := Config{URL: url, CAFile: filepath.Join(f.dir, "ca", "ca.crt"), Name: "anonymous"}
	if _, err := NewSubscriber(anonymous).Stream(ctx, bustest.NodeA); err == nil {
		t.Error("a client without a certificate connected")
	}
	// A key without its certificate is a configuration error, not a retry.
	if _, err := options(Config{KeyFile: "k"}); err == nil {
		t.Error("a key without a certificate was accepted")
	}
	// And node A still reads its own.
	stream, err := NewSubscriber(f.nodeConfig(url, bustest.NodeA)).Stream(ctx, bustest.NodeA)
	if err != nil {
		t.Fatal(err)
	}
	<-stream // the resync
	if err := publisher.Push(ctx, bustest.NodeA, application.NodePush{Execution: "exec-own"}); err != nil {
		t.Fatal(err)
	}
	select {
	case push := <-stream:
		if push.Execution != "exec-own" {
			t.Errorf("push = %+v", push)
		}
		if push.Ack != nil {
			push.Ack()
		}
	case <-ctx.Done():
		t.Fatal("node A's own push never came")
	}
}
