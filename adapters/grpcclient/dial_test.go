package grpcclient

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/status"

	"github.com/underpass-ai/axlr-bots/adapters/unixapi"
	operatorv1 "github.com/underpass-ai/axlr-bots/api/gen/axlrbots/operator/v1"
)

// throwawayCA is a CA and a server certificate for 127.0.0.1 under it.
func throwawayCA(t *testing.T) (serverTLS *tls.Config, roots *x509.CertPool) {
	t.Helper()
	caKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	caTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "test fleet CA"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	caCert, _ := x509.ParseCertificate(caDER)
	serverKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	serverTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: "control plane"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	serverDER, err := x509.CreateCertificate(rand.Reader, serverTemplate, caCert, &serverKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	roots = x509.NewCertPool()
	roots.AddCert(caCert)
	return &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{{Certificate: [][]byte{serverDER}, PrivateKey: serverKey}}}, roots
}

func TestDialTLSChecksTheFleetCAAndSendsTheToken(t *testing.T) {
	serverTLS, roots := throwawayCA(t)
	fake := newFake()
	fake.token = testToken
	fake.answer(method("OverviewService", "GetBuild"), &operatorv1.GetBuildResponse{Running: &operatorv1.BuildIdentity{Version: "v"}})
	fake.answer(method("BotsService", "ListBots"), &operatorv1.ListBotsResponse{Bots: []*operatorv1.Bot{{Id: "bot-a"}}})
	server := fake.server(grpc.Creds(credentials.NewTLS(serverTLS)))
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)
	address := listener.Addr().String()

	client, err := DialTLS(address, &tls.Config{MinVersion: tls.VersionTLS13, RootCAs: roots}, testToken)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	bots, err := client.ListBots(t.Context())
	if err != nil || len(bots) != 1 || bots[0].ID != "bot-a" {
		t.Fatalf("over TLS with the token: %v %v", bots, err)
	}
	if got := fake.headers(method("BotsService", "ListBots")).Get("authorization"); len(got) != 1 || got[0] != "Bearer "+testToken {
		t.Errorf("authorization metadata: %v", got)
	}
	if err := client.Health(t.Context()); err != nil {
		t.Errorf("health: %v", err)
	}

	wrong, _ := DialTLS(address, &tls.Config{MinVersion: tls.VersionTLS13, RootCAs: roots}, "another")
	defer wrong.Close()
	if _, err := wrong.ListBots(t.Context()); !errors.As(err, new(unixapi.UnauthorizedError)) {
		t.Errorf("a wrong token is unauthorized: %v", err)
	}
	if err := wrong.Health(t.Context()); err != nil {
		t.Errorf("a control plane that refuses the token is still there: %v", err)
	}

	stranger, _ := DialTLS(address, &tls.Config{MinVersion: tls.VersionTLS13, RootCAs: x509.NewCertPool()}, testToken)
	defer stranger.Close()
	if _, err := stranger.ListBots(t.Context()); err == nil {
		t.Error("a certificate of another CA must not be trusted")
	}
	if err := stranger.Health(t.Context()); err == nil {
		t.Error("health must fail when the certificate is not trusted")
	}
}

func TestDialUnixTalksToTheDaemonSocketWithNoToken(t *testing.T) {
	fake := newFake()
	fake.answer(method("OverviewService", "GetBuild"), &operatorv1.GetBuildResponse{})
	fake.answer(method("BotsService", "ListBots"), &operatorv1.ListBotsResponse{})
	socket := filepath.Join(t.TempDir(), "cp-grpc.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	server := fake.server()
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)

	for _, address := range []string{socket, "unix://" + socket} {
		client, err := DialUnix(address)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := client.ListBots(t.Context()); err != nil {
			t.Errorf("%s: %v", address, err)
		}
		client.Close()
	}
	if got := fake.headers(method("BotsService", "ListBots")).Get("authorization"); len(got) != 0 {
		t.Errorf("the socket carries no token: %v", got)
	}
	if !Reachable(t.Context(), socket) {
		t.Error("a daemon answers on the socket")
	}
}

func TestReachableIsFalseWhenNothingListens(t *testing.T) {
	if Reachable(t.Context(), filepath.Join(t.TempDir(), "none.sock")) {
		t.Error("no daemon, not reachable")
	}
	dead := filepath.Join(t.TempDir(), "dead.sock")
	if listener, err := net.Listen("unix", dead); err == nil {
		listener.Close()
		_ = os.Remove(dead)
	}
	client, _ := DialUnix(dead)
	defer client.Close()
	err := client.Health(t.Context())
	if err == nil || err.Error() == "" {
		t.Errorf("health with nobody there: %v", err)
	}
}

func TestADaemonThatDoesNotKnowTheServiceStillAnswers(t *testing.T) {
	h := newHarness(t)
	// Nothing canned: the fake answers Unimplemented, a status, so it is there.
	if err := h.client.Health(t.Context()); err != nil {
		t.Errorf("health: %v", err)
	}
}

func TestADeadlineMakesTheProbeFail(t *testing.T) {
	h := newHarness(t)
	h.fake.fail(method("OverviewService", "GetBuild"), status.Error(codes.Unavailable, "down"))
	if err := h.client.Health(t.Context()); err == nil {
		t.Error("an unavailable control plane does not answer")
	}
}
