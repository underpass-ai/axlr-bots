package filestore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/underpass-ai/axlr-bots/domain"
)

const outputSecret = "a-webhook-secret-of-32-characters"

func TestOutputRepositoryKeepsTargetsPrivately(t *testing.T) {
	state := t.TempDir()
	repo := NewOutputRepository(state)
	ctx := context.Background()
	if targets, err := repo.List(ctx); err != nil || len(targets) != 0 {
		t.Fatalf("empty = %v %v", targets, err)
	}
	chat, _ := domain.NewOutputTarget(domain.OutputTargetSnapshot{Name: "chat", Type: "slack", URL: "https://hooks.slack.test/services/T/B/SECRET"})
	hook, _ := domain.NewOutputTarget(domain.OutputTargetSnapshot{Name: "hook", Type: "webhook", URL: "https://x.test/", Secret: outputSecret, Severities: []string{"sev1"}})
	for _, target := range []domain.OutputTarget{hook, chat} {
		if err := repo.Add(ctx, target); err != nil {
			t.Fatal(err)
		}
	}
	var conflict domain.ConflictError
	if err := repo.Add(ctx, chat); !errors.As(err, &conflict) {
		t.Errorf("added twice: %v", err)
	}
	info, err := os.Stat(filepath.Join(state, "outputs.json"))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("outputs.json: %v %v", err, info.Mode())
	}
	again := NewOutputRepository(state)
	targets, err := again.List(ctx)
	if err != nil || len(targets) != 2 || targets[0].Name() != "chat" || targets[1].Severities()[0] != domain.Sev1 {
		t.Fatalf("list = %v %v", targets, err)
	}
	if got, err := again.Get(ctx, "hook"); err != nil || got.Fingerprint() != hook.Fingerprint() {
		t.Errorf("get = %v", err)
	}
	if _, err := again.Get(ctx, "nope"); !domain.IsNotFound(err) {
		t.Errorf("get missing = %v", err)
	}
	if err := again.Remove(ctx, "chat"); err != nil {
		t.Fatal(err)
	}
	if err := again.Remove(ctx, "chat"); !domain.IsNotFound(err) {
		t.Errorf("removed twice = %v", err)
	}
	if targets, _ := again.List(ctx); len(targets) != 1 {
		t.Errorf("after remove = %d", len(targets))
	}
}

// A file the store cannot read, or one the domain refuses, fails every
// read loudly: a target half used would announce to the wrong place.
func TestOutputRepositoryRefusesABrokenFile(t *testing.T) {
	ctx := context.Background()
	for name, content := range map[string]string{
		"not json": "{",
		"refused":  `{"outputs":[{"name":"x","type":"email"}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			state := t.TempDir()
			if err := os.WriteFile(filepath.Join(state, "outputs.json"), []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
			repo := NewOutputRepository(state)
			if _, err := repo.List(ctx); err == nil || !strings.Contains(err.Error(), "outputs.json") {
				t.Errorf("list = %v", err)
			}
			if _, err := repo.Get(ctx, "x"); err == nil {
				t.Error("get passed")
			}
			target, _ := domain.NewOutputTarget(domain.OutputTargetSnapshot{Name: "y", Type: "command", Command: []string{"/bin/true"}})
			if err := repo.Add(ctx, target); err == nil {
				t.Error("add passed")
			}
			if err := repo.Remove(ctx, "x"); err == nil {
				t.Error("remove passed")
			}
		})
	}
	state := t.TempDir()
	if err := os.Mkdir(filepath.Join(state, "outputs.json"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := NewOutputRepository(state).List(ctx); err == nil {
		t.Error("a directory was read")
	}
}

func TestOutputDeliveryRepositoryMovesADeliveryOnceItIsDone(t *testing.T) {
	state := t.TempDir()
	repo := NewOutputDeliveryRepository(state)
	ctx := context.Background()
	if pending, err := repo.ListPending(ctx); err != nil || len(pending) != 0 {
		t.Fatalf("empty = %v %v", pending, err)
	}
	if _, err := repo.Get(ctx, "dlv-1"); !domain.IsNotFound(err) {
		t.Errorf("missing = %v", err)
	}
	target, _ := domain.NewOutputTarget(domain.OutputTargetSnapshot{Name: "relay", Type: "command", Command: []string{"/bin/true"}})
	incident, _ := domain.NewIncident("inc-1", "shop", "triage", "g", "manual", "t", domain.Sev2, nil, t0)
	d, _ := domain.NewOutputDelivery("dlv-1", target, incident, "evt-1", domain.EventIncidentOpened, json.RawMessage(`{"a":1}`), t0)
	other, _ := domain.NewOutputDelivery("dlv-2", target, incident, "evt-2", domain.EventIncidentResolved, json.RawMessage(`{}`), t0)
	for _, delivery := range []domain.OutputDelivery{d, other} {
		if err := repo.Save(ctx, delivery); err != nil {
			t.Fatal(err)
		}
	}
	if pending, err := repo.ListPending(ctx); err != nil || len(pending) != 2 || pending[0].ID() != "dlv-1" {
		t.Fatalf("pending = %v %v", pending, err)
	}
	_ = d.Delivered(t0.Add(time.Second))
	if err := repo.Save(ctx, d); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(state, "deliveries", "outputs", "pending", "dlv-1.json")); !errors.Is(err, os.ErrNotExist) {
		t.Error("a delivered delivery stayed pending")
	}
	if got, err := repo.Get(ctx, "dlv-1"); err != nil || got.Status() != domain.DeliveryDelivered {
		t.Errorf("get done = %v", err)
	}
	// The approvals' outbox does not see it.
	if pending, _ := NewDeliveryRepository(state).ListPending(ctx); len(pending) != 0 {
		t.Errorf("the approvals' outbox lists %d", len(pending))
	}
	dir := filepath.Join(state, "deliveries", "outputs", "pending")
	if err := os.WriteFile(filepath.Join(dir, "dlv-3.json"), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "dlv-4.json"), []byte(`{"id":"dlv-4","status":"lost"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if pending, err := repo.ListPending(ctx); err != nil || len(pending) != 1 || !quarantined(t, dir, "dlv-3") || !quarantined(t, dir, "dlv-4") {
		t.Errorf("past corrupt deliveries = %v %d", err, len(pending))
	}
	if err := repo.Save(ctx, domain.OutputDelivery{}); err == nil {
		t.Error("a delivery without an id was saved")
	}
}

// The pending deliveries come oldest first, whatever their ids' order
// (review of #136: proposed, diagnosed, resolved, opened).
func TestOutputDeliveryRepositoryListsOldestFirst(t *testing.T) {
	repo := NewOutputDeliveryRepository(t.TempDir())
	target, _ := domain.NewOutputTarget(domain.OutputTargetSnapshot{Name: "ops", Type: "slack", URL: "https://hooks.slack.test/services/T/B/X"})
	incident, _ := domain.NewIncident("inc-1", "ops", "triage", "g", "manual", "t", domain.Sev2, nil, t0)
	events := []domain.EventType{domain.EventIncidentOpened, domain.EventIncidentDiagnosed, domain.EventIncidentProposed, domain.EventIncidentResolved}
	for i, event := range events {
		id := domain.EventID("evt-" + string(rune('a'+i)))
		sum := sha256.Sum256([]byte("incident.output\x00" + id.String() + "\x00ops"))
		d, err := domain.NewOutputDelivery(domain.DeliveryID("dlv-"+hex.EncodeToString(sum[:12])), target, incident, id, event, []byte(`{}`), t0.Add(time.Duration(i)*time.Second))
		if err != nil {
			t.Fatal(err)
		}
		if err := repo.Save(context.Background(), d); err != nil {
			t.Fatal(err)
		}
	}
	list, err := repo.ListPending(context.Background())
	if err != nil || len(list) != len(events) {
		t.Fatalf("list = %d %v", len(list), err)
	}
	for i, d := range list {
		if d.Event() != events[i] {
			t.Errorf("%d: %s, want %s", i, d.Event(), events[i])
		}
	}
}
