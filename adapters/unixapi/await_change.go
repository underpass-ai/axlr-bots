package unixapi

import (
	"context"
	"time"
)

// awaitChange returns when signal closes, after d, or when ctx ends;
// false when ctx ended.
func awaitChange(ctx context.Context, signal <-chan struct{}, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-signal:
	case <-timer.C:
	}
	return true
}
