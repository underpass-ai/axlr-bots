package application

import (
	"strconv"
	"strings"
	"time"
)

const cursorPrefix = "a1."

// activityCursor is the instant of the newest change a watch has told, as
// text. A cursor holds no state of the server: a resumed watch reads the
// tree and tells what is newer than it.
func activityCursor(at time.Time) string {
	return cursorPrefix + strconv.FormatInt(at.UnixNano(), 10)
}

// parseActivityCursor reads a cursor; false when it is not one of ours.
func parseActivityCursor(cursor string) (time.Time, bool) {
	rest, ok := strings.CutPrefix(cursor, cursorPrefix)
	if !ok {
		return time.Time{}, false
	}
	n, err := strconv.ParseInt(rest, 10, 64)
	if err != nil || n <= 0 {
		return time.Time{}, false
	}
	return time.Unix(0, n).UTC(), true
}
