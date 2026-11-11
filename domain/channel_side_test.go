package domain

import "testing"

func TestParseChannelSide(t *testing.T) {
	for _, raw := range []string{"asked", "answered"} {
		if side, err := ParseChannelSide(raw); err != nil || side.String() != raw {
			t.Fatalf("%s: %v", raw, err)
		}
	}
	if _, err := ParseChannelSide("Asked"); err == nil {
		t.Fatal("accepted")
	}
}
