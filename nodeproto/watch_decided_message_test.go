package nodeproto

import (
	"testing"

	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

func TestAWatchDecisionsOutcomeRoundTrips(t *testing.T) {
	decided := application.WatchDecided{Question: "task-9", Choice: "abandon", Status: domain.TaskCancelled, Abandoned: true, Reason: "abandoned by qa-vigia: Jev chose abandon (p=0.83, c=0.71)"}
	back, err := ToWatchDecided(FromWatchDecided(decided))
	if err != nil || back != decided {
		t.Fatalf("back = %+v %v", back, err)
	}
	if _, err := ToWatchDecided(WatchDecidedMessage{Question: "task-9", Status: "Lost"}); err == nil {
		t.Error("a bad status read back")
	}
	if _, err := ToWatchDecided(WatchDecidedMessage{Question: "Bad Id", Status: "Pending"}); err == nil {
		t.Error("a bad question read back")
	}
}
