package daemon

import (
	"context"
	"errors"
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/underpass-ai/axlr-bots/adapters/axlr"
	"github.com/underpass-ai/axlr-bots/adapters/nodeagent"
	"github.com/underpass-ai/axlr-bots/adapters/pki"
	"github.com/underpass-ai/axlr-bots/adapters/unixapi"
	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
	"github.com/underpass-ai/axlr-bots/internal/fakeaxlr"
	"github.com/underpass-ai/axlr-bots/nodeproto"
)

// startTLS runs the daemon on the socket and a TLS address on the
// loopback, with the fleet CA under the state directory.
func (f *fixture) startTLS(t *testing.T) (*unixapi.Client, string, func()) {
	t.Helper()
	ca, err := pki.Generate(filepath.Join(f.state, pki.CADirName))
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, f.deps, Options{SocketPath: f.socket, Listener: listener, TLS: pki.ServerTLSConfig(ca, []string{"127.0.0.1"}), Tick: 20 * time.Millisecond, ReconcileEvery: 50 * time.Millisecond, Grace: time.Hour, NodeOfflineAfter: time.Second, Log: log.New(f.logs, "", 0)})
	}()
	for !unixapi.Reachable(context.Background(), f.socket) {
		time.Sleep(10 * time.Millisecond)
	}
	return unixapi.NewClient(f.socket), "https://" + listener.Addr().String(), func() {
		cancel()
		if err := <-done; err != nil {
			t.Error(err)
		}
	}
}

// enrolledAgent enrolls a node with a token and returns an agent that
// speaks to the control plane over TLS with the certificate it got.
func enrolledAgent(t *testing.T, operator *unixapi.Client, url, nodeID string, labels map[string]string, axlrBin, nodeDir string) nodeagent.Agent {
	t.Helper()
	ctx := context.Background()
	minted, err := operator.CreateEnrollmentToken(ctx, unixapi.EnrollTokenRequest{NodeID: nodeID, Labels: labels})
	if err != nil {
		t.Fatal(err)
	}
	token, err := nodeproto.DecodeEnrollToken(minted.Token)
	if err != nil {
		t.Fatal(err)
	}
	key, csr, _ := pki.NewNodeKeyAndRequest(domain.NodeID(nodeID))
	enrollConfig, _ := pki.EnrollTLSConfig([]byte(token.CAPEM))
	response, err := unixapi.NewTLSClient(url, enrollConfig).Enroll(ctx, nodeproto.EnrollRequest{TokenID: token.ID, Secret: token.Secret, HostName: nodeID + ".example", CSR: string(csr)})
	if err != nil {
		t.Fatal(err)
	}
	if err := pki.SaveCredentials(nodeDir, key, []byte(response.Certificate), []byte(response.CA)); err != nil {
		t.Fatal(err)
	}
	creds, err := pki.LoadCredentials(nodeDir)
	if err != nil {
		t.Fatal(err)
	}
	config, _ := creds.ClientTLSConfig()
	return nodeagent.Agent{
		Transport: unixapi.NewTLSClient(url, config),
		Executor:  axlr.NewExecutor(axlrBin, nodeDir),
		Options:   nodeagent.Options{NodeID: creds.NodeID, Labels: response.Node.Labels, Heartbeat: 50 * time.Millisecond, PollWait: 200 * time.Millisecond, Renew: 30 * time.Millisecond, Log: log.New(os.Stderr, nodeID+" ", 0)},
	}
}

// twoNodesBySelector starts the control plane over TLS and two enrolled
// agents in different zones, and sees a bot placed by selector run on
// the node its selector names.
func twoNodesBySelector(t *testing.T, axlrBin string, objective string) {
	t.Helper()
	f := newFixture(t, "completed")
	operator, url, stop := f.startTLS(t)
	defer stop()
	ctx, cancelAgents := context.WithCancel(context.Background())
	defer cancelAgents()
	agents := map[string]string{"node-eu": "eu", "node-us": "us"}
	done := make(chan error, len(agents))
	for id, zone := range agents {
		agent := enrolledAgent(t, operator, url, id, map[string]string{"zone": zone}, axlrBin, filepath.Join(f.dir, id))
		go func() { done <- agent.Run(ctx) }()
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		nodes, _ := operator.ListNodes(context.Background())
		online := 0
		for _, n := range nodes {
			if n.Status == "Online" && n.CertificateSerial != "" {
				online++
			}
		}
		if online == 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("nodes not online: %+v", nodes)
		}
		time.Sleep(20 * time.Millisecond)
	}
	bot := application.BotDefinition{Tenant: "t", Name: "bot-us", Owner: "o", Mission: "do what each task asks", AllowedTools: []string{"local_list"}, Placement: domain.PlacementSnapshot{Selector: map[string]string{"zone": "us"}}, Model: application.ModelPolicyDefinition{Model: "anthropic/claude-haiku-5.5"}}
	if _, err := operator.RegisterBot(context.Background(), bot); err != nil {
		t.Fatal(err)
	}
	if _, err := operator.EnableBot(context.Background(), "bot-us"); err != nil {
		t.Fatal(err)
	}
	submitted, err := operator.SubmitTask(context.Background(), unixapi.SubmitRequest{BotID: "bot-us", Objective: objective, IdempotencyKey: "k-us"})
	if err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(3 * time.Minute)
	var task domain.TaskSnapshot
	for {
		task, _ = operator.GetTask(context.Background(), submitted.Task.ID)
		if task.Status == "Completed" || task.Status == "Failed" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("task is %s: %s", task.Status, f.logs.String())
		}
		time.Sleep(100 * time.Millisecond)
	}
	execution, err := operator.GetExecution(context.Background(), task.ExecutionIDs[0])
	if err != nil || execution.NodeID != "node-us" {
		t.Fatalf("execution = %+v %v", execution, err)
	}
	if task.Status != "Completed" {
		t.Fatalf("task failed: %s", execution.FailureReason)
	}
	t.Logf("task %s ran on %s: %q (cost %v)", task.ID, execution.NodeID, strings.SplitN(execution.Result.Answer, "\n", 2)[0], execution.Usage.CostUSD)
	cancelAgents()
	for range agents {
		if err := <-done; err != nil {
			t.Error(err)
		}
	}
}

func TestTwoNodesOverTLSRunTheBotWhereItsSelectorSays(t *testing.T) {
	bin, _, err := fakeaxlr.Write(t.TempDir(), fakeaxlr.Options{Scenario: "completed"})
	if err != nil {
		t.Fatal(err)
	}
	twoNodesBySelector(t, bin, "List the files in your workspace")
}

// TestLiveTwoNodesOverTLS is the same with the real axlr-tui and a
// model key: AXLR_BOTS_LIVE=1 AXLR_TUI=/path/to/axlr-tui.
func TestLiveTwoNodesOverTLS(t *testing.T) {
	if os.Getenv("AXLR_BOTS_LIVE") != "1" {
		t.Skip("set AXLR_BOTS_LIVE=1 (and AXLR_TUI, OPENROUTER_API_KEY) to run the live test")
	}
	bin := os.Getenv("AXLR_TUI")
	if bin == "" {
		t.Fatal("AXLR_TUI names the axlr-tui to run")
	}
	twoNodesBySelector(t, bin, "List the files in your workspace and say how many there are.")
}

func TestDaemonTLSOptions(t *testing.T) {
	f := newFixture(t, "completed")
	if err := Run(context.Background(), f.deps, Options{SocketPath: f.socket, ListenAddr: "127.0.0.1:0"}); err == nil || !strings.Contains(err.Error(), "TLS configuration") {
		t.Errorf("listen without TLS: %v", err)
	}
	ca, _ := pki.Generate(filepath.Join(f.state, pki.CADirName))
	if err := Run(context.Background(), f.deps, Options{SocketPath: f.socket, ListenAddr: "300.0.0.1:0", TLS: pki.ServerTLSConfig(ca, nil)}); err == nil {
		t.Error("a bad address listened")
	}
	// A ListenAddr the daemon binds itself.
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, f.deps, Options{SocketPath: f.socket, ListenAddr: "127.0.0.1:0", TLS: pki.ServerTLSConfig(ca, []string{"127.0.0.1"}), Tick: 20 * time.Millisecond, Log: log.New(f.logs, "", 0)})
	}()
	for !unixapi.Reachable(context.Background(), f.socket) {
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	if err := <-done; err != nil {
		t.Error(err)
	}
	if !strings.Contains(f.logs.String(), "and https://127.0.0.1:") {
		t.Errorf("logs = %s", f.logs.String())
	}
}

func TestDaemonOperatorAddressOptions(t *testing.T) {
	f := newFixture(t, "completed")
	if err := Run(context.Background(), f.deps, Options{SocketPath: f.socket, APIListenAddr: "127.0.0.1:0", APIToken: "t"}); err == nil || !strings.Contains(err.Error(), "TLS configuration") {
		t.Errorf("operator address without TLS: %v", err)
	}
	ca, _ := pki.Generate(filepath.Join(f.state, pki.CADirName))
	tlsConfig := pki.ServerTLSConfig(ca, []string{"127.0.0.1"})
	if err := Run(context.Background(), f.deps, Options{SocketPath: f.socket, APIListenAddr: "127.0.0.1:0", TLS: tlsConfig}); err == nil || !strings.Contains(err.Error(), "bearer token") {
		t.Errorf("operator address without a token: %v", err)
	}
	if err := Run(context.Background(), f.deps, Options{SocketPath: f.socket, ListenAddr: "127.0.0.1:0", APIListenAddr: "300.0.0.1:0", APIToken: "t", TLS: tlsConfig}); err == nil {
		t.Error("a bad operator address listened")
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, f.deps, Options{SocketPath: f.socket, APIListener: listener, APIToken: "operator-token", TLS: tlsConfig, Tick: 20 * time.Millisecond, Log: log.New(f.logs, "", 0)})
	}()
	for !unixapi.Reachable(context.Background(), f.socket) {
		time.Sleep(10 * time.Millisecond)
	}
	clientConfig, _ := pki.EnrollTLSConfig(ca.CertificatePEM())
	url := "https://" + listener.Addr().String()
	if bots, err := unixapi.NewTLSClient(url, clientConfig.Clone()).WithToken("operator-token").ListBots(context.Background()); err != nil || len(bots) != 0 {
		t.Errorf("operator address with the token: %v", err)
	}
	var unauthorized unixapi.UnauthorizedError
	if _, err := unixapi.NewTLSClient(url, clientConfig.Clone()).ListBots(context.Background()); !errors.As(err, &unauthorized) {
		t.Errorf("operator address without the token: %v", err)
	}
	cancel()
	if err := <-done; err != nil {
		t.Error(err)
	}
	if !strings.Contains(f.logs.String(), "(operators, bearer token)") {
		t.Errorf("logs = %s", f.logs.String())
	}
}
