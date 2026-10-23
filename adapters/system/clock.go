// Package system holds the adapters that depend on the machine: the wall
// clock and random identifiers.
package system

import "time"

// Clock tells the wall-clock time in UTC.
type Clock struct{}

// Now returns the current time in UTC.
func (Clock) Now() time.Time { return time.Now().UTC() }
