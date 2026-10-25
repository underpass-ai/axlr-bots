package notify

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

var _ application.NotificationSender = (*Sender)(nil)

const secret = "0123456789abcdef-hook"

var t0 = time.Date(2026, 10, 25, 9, 0, 0, 0, time.UTC)

func delivery(t *testing.T, target domain.NotifyTarget) domain.Delivery {
	t.Helper()
	payload := json.RawMessage(`{"schema":"axlr-bots.approval-request/v1","taskId":"task-1","call":{"tool":"local_write","callId":"c1"}}`)
	d, err := domain.NewDelivery("dlv-abc", "bot-1", "task-1", "exec-1", "c1", target, payload, t0)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func webhookTo(t *testing.T, url string) domain.NotifyTarget {
	t.Helper()
	target, err := domain.NewNotifyTarget(domain.NotifyTargetSnapshot{Type: "webhook", Name: "ops", URL: url, Secret: secret})
	if err != nil {
		t.Fatal(err)
	}
	return target
}

// receiver is a webhook that answers with the scripted statuses in turn
// and checks every request the way a receiver should: the signature
// over the body under the shared secret, and the delivery id.
type receiver struct {
	mu       sync.Mutex
	statuses []int
	bodies   [][]byte
	ids      []string
	valid    []bool
}

func (r *receiver) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	body, _ := io.ReadAll(req.Body)
	trigger, _ := domain.NewTrigger(domain.TriggerSnapshot{Type: "webhook", SigningKey: secret, Objective: "check"})
	r.mu.Lock()
	defer r.mu.Unlock()
	r.bodies = append(r.bodies, body)
	r.ids = append(r.ids, req.Header.Get(DeliveryHeader))
	r.valid = append(r.valid, trigger.Authenticates("", req.Header.Get(SignatureHeader), body) &&
		req.Header.Get("Content-Type") == "application/json" && req.Header.Get(EventHeader) == "bot.task.waiting_approval" &&
		req.Header.Get("Idempotency-Key") == req.Header.Get(DeliveryHeader))
	status := http.StatusOK
	if len(r.statuses) > 0 {
		status, r.statuses = r.statuses[0], r.statuses[1:]
	}
	w.WriteHeader(status)
	if status >= 300 {
		_, _ = w.Write([]byte("busy\nmore"))
	}
}

func TestAWebhookDeliveryIsSignedAndRetriedUntilAccepted(t *testing.T) {
	r := &receiver{statuses: []int{http.StatusServiceUnavailable, http.StatusBadGateway, http.StatusAccepted}}
	server := httptest.NewServer(r)
	defer server.Close()
	target := webhookTo(t, server.URL+"/hooks/axlr")
	d := delivery(t, target)
	sender := NewSender()
	ctx := context.Background()
	var errs []error
	for range 3 {
		errs = append(errs, sender.Send(ctx, target, d))
	}
	if errs[0] == nil || errs[0].Error() != "HTTP 503: busy" || errs[1] == nil || errs[2] != nil {
		t.Fatalf("attempts = %v", errs)
	}
	if len(r.bodies) != 3 {
		t.Fatalf("requests = %d", len(r.bodies))
	}
	for i := range r.bodies {
		if !r.valid[i] || r.ids[i] != "dlv-abc" || string(r.bodies[i]) != string(d.Payload()) {
			t.Errorf("attempt %d: valid %v id %s body %s", i+1, r.valid[i], r.ids[i], r.bodies[i])
		}
	}
	// A receiver with another secret refuses the signature.
	other, _ := domain.NewTrigger(domain.TriggerSnapshot{Type: "webhook", SigningKey: "another-secret-0123456", Objective: "x"})
	if other.Authenticates("", target.Sign(r.bodies[0]), r.bodies[0]) {
		t.Error("the signature verified under another secret")
	}
}

func TestAWebhookRedirectIsNotFollowed(t *testing.T) {
	followed := false
	elsewhere := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { followed = true }))
	defer elsewhere.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		http.Redirect(w, req, elsewhere.URL, http.StatusTemporaryRedirect)
	}))
	defer server.Close()
	target := webhookTo(t, server.URL)
	err := NewSender().Send(context.Background(), target, delivery(t, target))
	if err == nil || !strings.HasPrefix(err.Error(), "HTTP 307") || followed {
		t.Errorf("redirect: %v (followed %v)", err, followed)
	}
}

func TestAWebhookThatDoesNotAnswerFails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		select {
		case <-req.Context().Done():
		case <-time.After(2 * time.Second):
		}
	}))
	defer server.Close()
	target := webhookTo(t, server.URL)
	sender := &Sender{Timeout: 50 * time.Millisecond}
	if err := sender.Send(context.Background(), target, delivery(t, target)); err == nil {
		t.Error("a webhook that never answered was delivered")
	}
	closed := httptest.NewServer(http.NotFoundHandler())
	closed.Close()
	target = webhookTo(t, closed.URL)
	if err := NewSender().Send(context.Background(), target, delivery(t, target)); err == nil {
		t.Error("a closed port was delivered")
	}
	silent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusInternalServerError) }))
	defer silent.Close()
	target = webhookTo(t, silent.URL)
	if err := NewSender().Send(context.Background(), target, delivery(t, target)); err == nil || err.Error() != "HTTP 500" {
		t.Errorf("500 without a body: %v", err)
	}
}

func commandTarget(t *testing.T, words ...string) domain.NotifyTarget {
	t.Helper()
	target, err := domain.NewNotifyTarget(domain.NotifyTargetSnapshot{Type: "command", Name: "desk", Command: words})
	if err != nil {
		t.Fatal(err)
	}
	return target
}

func TestACommandReceivesTheNotificationOnStdin(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "notification.json")
	env := filepath.Join(dir, "env")
	target := commandTarget(t, "/bin/sh", "-c", `cat > "$1" && printf '%s %s' "$AXLR_BOTS_DELIVERY_ID" "$AXLR_BOTS_TASK_ID" > "$2"`, "notify", out, env)
	d := delivery(t, target)
	if err := NewSender().Send(context.Background(), target, d); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(out); string(got) != string(d.Payload()) {
		t.Errorf("stdin = %s", got)
	}
	if got, _ := os.ReadFile(env); string(got) != "dlv-abc task-1" {
		t.Errorf("environment = %s", got)
	}
}

func TestACommandThatFailsSaysWhy(t *testing.T) {
	failing := commandTarget(t, "/bin/sh", "-c", `echo "no display" >&2; exit 4`)
	err := NewSender().Send(context.Background(), failing, delivery(t, failing))
	if err == nil || !strings.Contains(err.Error(), "exit status 4: no display") {
		t.Errorf("failing command: %v", err)
	}
	quiet := commandTarget(t, "/bin/sh", "-c", "exit 2")
	if err := NewSender().Send(context.Background(), quiet, delivery(t, quiet)); err == nil || !strings.Contains(err.Error(), "exit status 2") {
		t.Errorf("quiet failure: %v", err)
	}
	slow := commandTarget(t, "/bin/sh", "-c", "sleep 5")
	if err := (&Sender{Timeout: 100 * time.Millisecond}).Send(context.Background(), slow, delivery(t, slow)); err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Errorf("slow command: %v", err)
	}
	missing := commandTarget(t, "/nonexistent/notify")
	if err := NewSender().Send(context.Background(), missing, delivery(t, missing)); err == nil {
		t.Error("a missing program was delivered")
	}
	if err := NewSender().Send(context.Background(), domain.NotifyTarget{}, delivery(t, missing)); err == nil {
		t.Error("a target of no type was delivered")
	}
	noisy := commandTarget(t, "/bin/sh", "-c", `head -c 10000 /dev/zero | tr '\0' x >&2; exit 1`)
	if err := NewSender().Send(context.Background(), noisy, delivery(t, noisy)); err == nil || len(err.Error()) > 4200 {
		t.Errorf("stderr is bounded: %d", len(err.Error()))
	}
}
