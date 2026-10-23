package application

import "time"

// Clock tells the time, so use cases can be tested at a fixed instant.
type Clock interface {
	Now() time.Time
}
