package application

import "github.com/underpass-ai/axlr-bots/domain"

func ptr[T any](v T) *T { return &v }

// lastOf is the newest event of the kind.
func lastOf(h *harness, kind domain.EventType) (domain.Event, bool) {
	for n := len(h.log.events) - 1; n >= 0; n-- {
		if h.log.events[n].Type == kind {
			return h.log.events[n], true
		}
	}
	return domain.Event{}, false
}
