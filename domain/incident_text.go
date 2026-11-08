package domain

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// incidentTextPlaceholder stands for a text that had nothing left once
// cleaned.
const incidentTextPlaceholder = "(no usable text)"

// incidentTextCut ends a text that was cut to fit.
const incidentTextCut = "…"

// isIncidentTextControl reports a control character a task's words lose:
// C0 but newline and tab, DEL and C1, which a terminal reads as commands.
func isIncidentTextControl(r rune) bool {
	return (r < 0x20 && r != '\n' && r != '\t') || (r >= 0x7f && r <= 0x9f)
}

// IncidentTextIsEmpty reports what a task wrote that has nothing left
// once cleaned.
func IncidentTextIsEmpty(raw string) bool {
	return strings.TrimFunc(raw, func(r rune) bool { return isIncidentTextControl(r) || unicode.IsSpace(r) }) == ""
}

// IncidentText makes what a task wrote fit an incident's diagnosis or
// reason of at most max bytes: trimmed, without control characters other
// than newline and tab (DEL and C1 too), cut at a character boundary with a trailing "…"
// when too long, and never empty. A model's answer is not the system's to
// refuse: on 2 November 2026 an answer with a "\r" or longer than the
// limit left its incident Open with no task to move it.
func IncidentText(raw string, max int) string {
	text := strings.TrimSpace(strings.Map(func(r rune) rune {
		if isIncidentTextControl(r) {
			return -1
		}
		return r
	}, raw))
	if text == "" {
		return incidentTextPlaceholder
	}
	if len(text) <= max {
		return text
	}
	cut := max - len(incidentTextCut)
	for cut > 0 && !utf8.RuneStart(text[cut]) {
		cut--
	}
	return strings.TrimSpace(text[:cut]) + incidentTextCut
}
