package unixapi

import (
	"testing"

	"github.com/underpass-ai/axlr-bots/domain"
)

// Changed is taken before reading the state; the change that comes after
// closes it, and a heartbeat does not.
func TestChangedClosesOnTheNextChangeRecorded(t *testing.T) {
	local := &Local{changes: newChanges()}
	changed := local.Changed()
	select {
	case <-changed:
		t.Fatal("closed with nothing recorded")
	default:
	}
	local.changes.saw(domain.Event{Type: domain.EventNodeHeartbeat})
	select {
	case <-changed:
		t.Fatal("a heartbeat is no change")
	default:
	}
	local.changes.saw(domain.Event{Type: domain.EventTaskProgress})
	select {
	case <-changed:
	default:
		t.Fatal("a change did not close the channel")
	}
	select {
	case <-local.Changed():
		t.Fatal("the next channel starts open")
	default:
	}
}
