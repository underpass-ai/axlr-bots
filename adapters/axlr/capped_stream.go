package axlr

import (
	"time"
	"unicode/utf8"

	"github.com/underpass-ai/axlr-bots/application"
)

// cappedStream tells the pieces of one stream of a run, the model's words
// or its reasoning, until the run's cap for it, then one mark that the
// rest was cut and nothing more.
type cappedStream struct {
	kind, cutKind application.ProgressKind
	limit         int
	told          int
	cut           bool
}

// feed tells one piece through tell, cut at the cap.
func (s *cappedStream) feed(text string, now time.Time, tell func(application.ProgressItem)) {
	if s.cut || text == "" {
		return
	}
	if s.told+len(text) > s.limit {
		keep := s.limit - s.told
		for keep > 0 && !utf8.RuneStart(text[keep]) {
			keep--
		}
		if keep > 0 {
			tell(application.ProgressItem{Kind: s.kind, At: now, Text: text[:keep]})
		}
		s.told = s.limit
		s.cut = true
		tell(application.ProgressItem{Kind: s.cutKind, At: now})
		return
	}
	s.told += len(text)
	tell(application.ProgressItem{Kind: s.kind, At: now, Text: text})
}
