package domain

import (
	"slices"
	"testing"
)

// item 9: the rewrite belongs to reading old state, not to a definition written today.
func TestPR142_NamingTheEightEventsToday(t *testing.T) {
	var raw []string
	for _, e := range DefaultOutputEvents[:len(DefaultOutputEvents)-1] {
		raw = append(raw, e.String())
	}
	got, err := outputEvents("output", raw)
	if err != nil {
		t.Fatal(err)
	}
	if slices.Contains(got, EventIncidentPostmortemApproved) {
		t.Errorf("an operator naming exactly eight events is given a ninth: %v", got)
	}
	swapped := slices.Clone(raw)
	swapped[0], swapped[1] = swapped[1], swapped[0]
	other, err := outputEvents("output", swapped)
	if err != nil {
		t.Fatal(err)
	}
	if slices.Contains(other, EventIncidentPostmortemApproved) != slices.Contains(got, EventIncidentPostmortemApproved) {
		t.Errorf("the same set in another order reads differently: %d vs %d events", len(other), len(got))
	}
}
