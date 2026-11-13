package unixapi

import (
	"testing"

	"github.com/underpass-ai/axlr-bots/domain"
)

func TestActivityChangesWakeOnEveryEventRecorded(t *testing.T) {
	hub := newChanges()
	signal := activityChanges{hub: hub}.Next()
	select {
	case <-signal:
		t.Fatal("woken with nothing recorded")
	default:
	}
	hub.saw(domain.Event{Type: domain.EventNodeHeartbeat})
	select {
	case <-signal:
		t.Fatal("a heartbeat is no change")
	default:
	}
	hub.saw(domain.Event{Type: domain.EventTaskProgress})
	select {
	case <-signal:
	default:
		t.Fatal("an event did not wake the watch")
	}
	local := &Local{changes: hub}
	if local.ActivityChanges() == nil {
		t.Error("no signal")
	}
}
