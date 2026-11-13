package grpcclient

import (
	"testing"
	"time"

	"google.golang.org/protobuf/types/known/durationpb"
)

func TestDurationsAreWrittenAsTheBotFilesDo(t *testing.T) {
	for d, want := range map[time.Duration]string{
		15 * time.Minute: "15m", time.Hour: "1h", 90 * time.Minute: "1h30m", 2*time.Hour + 5*time.Minute: "2h5m", 45 * time.Second: "45s",
		time.Minute + 30*time.Second: "1m30s", 0: "0s", 36 * time.Hour: "36h",
	} {
		if got := durationText(durationpb.New(d)); got != want {
			t.Errorf("%v is %q, want %q", d, got, want)
		}
	}
	if durationText(nil) != "" || seconds(nil) != 0 || seconds(durationpb.New(2*time.Minute)) != 120 {
		t.Error("a missing duration is empty and zero seconds")
	}
	if d, err := durationOf("f", ""); d != nil || err != nil {
		t.Errorf("empty is unset: %v %v", d, err)
	}
	if d, err := durationOf("f", "1h30m"); err != nil || d.AsDuration() != 90*time.Minute {
		t.Errorf("parse: %v %v", d, err)
	}
	if stampOf(time.Time{}) != nil || timeOf(nil) != (time.Time{}) || timePtr(nil) != nil {
		t.Error("zero and missing instants are unset")
	}
}
