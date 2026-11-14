package application

import (
	"context"
	"testing"

	"github.com/underpass-ai/axlr-bots/domain"
)

func TestARefusedPublicationIsAuditedDenied(t *testing.T) {
	h := newHarness()
	uc := NewRefusePublication(h.deps)
	if err := uc.Execute(context.Background(), RefusedPublication{Tenant: "default", Team: "team-a", Name: "done.task", Key: "k-1", Reason: "done.* is the control plane's own"}); err != nil {
		t.Fatal(err)
	}
	if err := uc.Execute(context.Background(), RefusedPublication{Reason: "bad envelope"}); err != nil {
		t.Fatal(err)
	}
	if len(h.log.audits) != 2 {
		t.Fatalf("audits = %+v", h.log.audits)
	}
	first, second := h.log.audits[0], h.log.audits[1]
	if first.Action != "event.publish" || first.Outcome != domain.AuditDenied || first.Subject != "team-a" || first.Details["key"] != "k-1" || first.Details["tenant"] != "default" || first.Details["name"] != "done.task" {
		t.Errorf("first = %+v", first)
	}
	if second.Subject != "unknown" || second.Details["reason"] != "bad envelope" || len(second.Details) != 1 {
		t.Errorf("second = %+v", second)
	}
}
