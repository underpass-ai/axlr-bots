package domain

import (
	"strings"
	"testing"
)

func TestAnOperatorMessageIsTrimmedAndBounded(t *testing.T) {
	message, err := NewOperatorMessage("  check row F too  ")
	if err != nil || message.String() != "check row F too" {
		t.Fatalf("%q %v", message, err)
	}
	for _, bad := range []string{"", "   ", strings.Repeat("x", MaxOperatorMessageLength+1)} {
		if _, err := NewOperatorMessage(bad); err == nil {
			t.Fatalf("%q accepted", bad)
		}
	}
	if _, err := ParseEventType("bot.task.message"); err != nil {
		t.Fatal(err)
	}
}
