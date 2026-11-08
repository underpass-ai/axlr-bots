package application

import (
	"fmt"
	"strings"

	"github.com/underpass-ai/axlr-bots/domain"
)

// cutTextMark stands alone for a text whose room would not hold even
// the sentence saying what was cut.
const cutTextMark = "[cut to fit]"

// postmortemText is one text of a postmortem's objective, cut to fit in
// a stated way: a list loses its oldest items first, prose its end, and
// either says in the text what went.
type postmortemText struct {
	lines     []string // the list's items, oldest first; nil for prose
	recent    int      // lines[recent:] are the most recent cycle's
	omitted   int      // items left out before the objective was written
	noun      string
	separator string
	prose     string
}

// postmortemListOf builds a list. recent is the first item of the most
// recent cycle; when no item names it, the newest is kept as that.
func postmortemListOf(lines []string, recent, omitted int, noun, separator string) postmortemText {
	cleaned := make([]string, 0, len(lines))
	for _, line := range lines {
		cleaned = append(cleaned, domain.IncidentText(line, domain.MaxObjectiveLength))
	}
	if recent < 0 || recent >= len(cleaned) {
		recent = max(len(cleaned)-1, 0)
	}
	return postmortemText{lines: cleaned, recent: recent, omitted: omitted, noun: noun, separator: separator}
}

// postmortemProse builds a text cut from its end.
func postmortemProse(text string) postmortemText {
	if text == "" {
		return postmortemText{}
	}
	return postmortemText{prose: domain.IncidentText(text, len(text))}
}

func (t postmortemText) isList() bool { return t.lines != nil }

func (t postmortemText) empty() bool {
	return !t.isList() && t.prose == "" || t.isList() && len(t.lines) == 0
}

func (t postmortemText) total() int { return len(t.lines) + t.omitted }

// listMark says how many of the list's items are left out.
func (t postmortemText) listMark(left int) string {
	return fmt.Sprintf("[Cut to fit the prompt's limit: the oldest %d of %d %s are left out.]", left, t.total(), t.noun)
}

// proseMark says how much of a prose text is left out.
func (t postmortemText) proseMark(left int) string {
	return fmt.Sprintf("[Cut to fit the prompt's limit: the last %d of %d bytes of this text are left out.]", left, len(t.prose))
}

// recentBytes is the room the text keeps before any other's history:
// prose whole, a list's most recent cycle with the mark of what is
// older.
func (t postmortemText) recentBytes() int {
	if !t.isList() {
		return len(t.prose)
	}
	size := len(strings.Join(t.lines[t.recent:], t.separator))
	if t.recent > 0 || t.omitted > 0 {
		size += len(t.listMark(t.total())) + 1
	}
	return size
}

// wholeBytes is the room the text needs to lose nothing more.
func (t postmortemText) wholeBytes() int {
	if !t.isList() {
		return len(t.prose)
	}
	size := len(strings.Join(t.lines, t.separator))
	if t.omitted > 0 {
		size += len(t.listMark(t.total())) + 1
	}
	return size
}

// render writes the text in room bytes.
func (t postmortemText) render(room int) string {
	if t.isList() {
		return t.renderList(room)
	}
	if len(t.prose) <= room {
		return t.prose
	}
	keep := room - len(t.proseMark(len(t.prose))) - 1
	if keep < minPhaseObjectiveText {
		return cutTextMark
	}
	kept := domain.IncidentText(t.prose, keep)
	return kept + "\n" + t.proseMark(len(t.prose)-len(kept))
}

// renderList keeps the newest items that fit with the mark, in order.
func (t postmortemText) renderList(room int) string {
	whole := strings.Join(t.lines, t.separator)
	if t.omitted == 0 && len(whole) <= room {
		return whole
	}
	budget := room - len(t.listMark(t.total())) - 1
	if budget < 0 {
		return cutTextMark
	}
	kept, used := 0, 0
	for kept < len(t.lines) {
		size := len(t.lines[len(t.lines)-1-kept])
		if kept > 0 {
			size += len(t.separator)
		}
		if used+size > budget {
			break
		}
		used += size
		kept++
	}
	mark := t.listMark(t.total() - kept)
	if kept == 0 {
		return mark
	}
	return mark + "\n" + strings.Join(t.lines[len(t.lines)-kept:], t.separator)
}

// postmortemTextFit cuts the texts to fit room together, in two passes.
// First each keeps what it cannot lose without saying so (the most
// recent cycle's items, a person's note and a draft, the diagnosis and
// the proposal), sharing the room so a text shorter than its share keeps
// all of it. Then what is left goes to the lists' older items, oldest
// lost first. A text never holds more than the room it was given; only
// the floor of a share can pass a room a template left almost none of,
// and a task that does not fit is refused at its assignment.
func postmortemTextFit(texts map[string]postmortemText, room int) map[string]string {
	recent := make(map[string]int, len(texts))
	for key, text := range texts {
		recent[key] = text.recentBytes()
	}
	rooms := shareBytes(recent, room, minPhaseObjectiveText)
	left := room
	for _, given := range rooms {
		left -= given
	}
	more := map[string]int{}
	for key, text := range texts {
		if extra := text.wholeBytes() - rooms[key]; extra > 0 {
			more[key] = extra
		}
	}
	if left > 0 {
		for key, extra := range shareBytes(more, left, 0) {
			rooms[key] += extra
		}
	}
	out := make(map[string]string, len(texts))
	for key, text := range texts {
		out[key] = text.render(rooms[key])
	}
	return out
}

// shareBytes shares room among what each key wants: a key wanting less
// than its share gets what it wants and leaves the rest to the others.
// floor is the least a share may be.
func shareBytes(wants map[string]int, room, floor int) map[string]int {
	out := make(map[string]int, len(wants))
	left := make(map[string]int, len(wants))
	for key, want := range wants {
		left[key] = want
	}
	for len(left) > 0 {
		share := max(room/len(left), floor)
		settled := false
		for key, want := range left {
			if want <= share {
				out[key] = want
				room -= want
				delete(left, key)
				settled = true
			}
		}
		if !settled {
			for key := range left {
				out[key] = share
			}
			break
		}
	}
	return out
}
