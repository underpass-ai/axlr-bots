package grpcclient

import (
	"fmt"
	"strings"
	"time"

	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/underpass-ai/axlr-bots/domain"
)

// timeOf is the instant of a timestamp; a missing one is the zero time.
func timeOf(stamp *timestamppb.Timestamp) time.Time {
	if stamp == nil {
		return time.Time{}
	}
	return stamp.AsTime()
}

// timePtr is timeOf for a field that is a pointer in the domain: a
// missing timestamp is nil, so omitempty keeps the field out.
func timePtr(stamp *timestamppb.Timestamp) *time.Time {
	if stamp == nil {
		return nil
	}
	instant := stamp.AsTime()
	return &instant
}

// stampOf is the timestamp of an instant; the zero time is unset.
func stampOf(instant time.Time) *timestamppb.Timestamp {
	if instant.IsZero() {
		return nil
	}
	return timestamppb.New(instant)
}

// durationText writes a duration as the bot files do: 1h, 15m, 90s, not
// 1h0m0s. A missing one is empty.
func durationText(d *durationpb.Duration) string {
	if d == nil {
		return ""
	}
	text := d.AsDuration().String()
	if strings.HasSuffix(text, "m0s") {
		text = strings.TrimSuffix(text, "0s")
	}
	if strings.HasSuffix(text, "h0m") {
		text = strings.TrimSuffix(text, "0m")
	}
	return text
}

// durationOf reads a duration the way durationText writes it; empty is
// unset, and text that is no duration is a validation error naming the
// field.
func durationOf(field, text string) (*durationpb.Duration, error) {
	if text == "" {
		return nil, nil
	}
	parsed, err := time.ParseDuration(text)
	if err != nil {
		return nil, domain.ValidationError{Field: field, Reason: fmt.Sprintf("%q is not a duration such as 15m", text)}
	}
	return durationpb.New(parsed), nil
}

// seconds is the whole seconds of a duration; a missing one is zero.
func seconds(d *durationpb.Duration) int {
	if d == nil {
		return 0
	}
	return int(d.AsDuration() / time.Second)
}
