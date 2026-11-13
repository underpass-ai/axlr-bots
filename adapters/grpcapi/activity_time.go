package grpcapi

import (
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"
)

// activityTime is an instant as a protobuf timestamp; the zero time is
// absent.
func activityTime(t time.Time) *timestamppb.Timestamp {
	if t.IsZero() {
		return nil
	}
	return timestamppb.New(t)
}
