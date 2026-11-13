package application

import "testing"

func TestOverLaysOnlyTheFieldsThatAreSet(t *testing.T) {
	base := LogSubject{Bot: "a", Node: "n", Task: "t", Execution: "e", CorrelationID: "c"}
	if got := base.Over(LogSubject{}); got != base {
		t.Errorf("empty over base: %v", got)
	}
	got := base.Over(LogSubject{Bot: "b", Node: "m", Task: "u", Execution: "f", CorrelationID: "d"})
	if got != (LogSubject{Bot: "b", Node: "m", Task: "u", Execution: "f", CorrelationID: "d"}) {
		t.Errorf("full over base: %v", got)
	}
}
