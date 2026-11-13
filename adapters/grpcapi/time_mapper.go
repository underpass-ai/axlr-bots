package grpcapi

import (
	"time"

	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// timestamp is the instant on the wire; the zero time is absent.
func timestamp(t time.Time) *timestamppb.Timestamp {
	if t.IsZero() {
		return nil
	}
	return timestamppb.New(t)
}

func timestampOf(t *time.Time) *timestamppb.Timestamp {
	if t == nil {
		return nil
	}
	return timestamp(*t)
}

// instant is the time of a timestamp; absent is the zero time.
func instant(ts *timestamppb.Timestamp) time.Time {
	if ts == nil {
		return time.Time{}
	}
	return ts.AsTime()
}

// durationText reads a duration kept as text (such as 15m); text that is
// not one, or empty, is absent.
func durationText(text string) *durationpb.Duration {
	d, err := time.ParseDuration(text)
	if text == "" || err != nil {
		return nil
	}
	return durationpb.New(d)
}

// textOf writes a duration as text a bot definition takes; absent or zero
// is empty.
func textOf(d *durationpb.Duration) string {
	if d.AsDuration() == 0 {
		return ""
	}
	return d.AsDuration().String()
}

func seconds(n int) *durationpb.Duration {
	if n == 0 {
		return nil
	}
	return durationpb.New(time.Duration(n) * time.Second)
}
