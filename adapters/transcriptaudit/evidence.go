package transcriptaudit

import (
	"regexp"
	"strings"
)

// Bounds of a flagged block's evidence.
const (
	maxEvidence      = 8
	evidenceBytes    = 600
	evidenceLeadings = 200
)

// failureSign matches what shows a failure in a transcript line, with
// JSON's escapes in between (`\"ok\": false`): fixed rules, so the reader
// of an event sees the lines that matter without the block.
var failureSign = regexp.MustCompile(`(?i)forbidden|refused|refuses|denied|\berror\b|\bfailed\b|exit_code\W{1,8}[1-9]|\bok\W{1,8}false|not found|no such|traceback|panic|timed out|timeout|cannot|could not`)

// evidence lists the lines of a block that show a failure, each cut to
// evidenceBytes around its first sign, at most maxEvidence.
func evidence(block string) []string {
	out := []string{}
	for _, text := range strings.Split(block, "\n") {
		where := failureSign.FindStringIndex(text)
		if where == nil {
			continue
		}
		start := max(where[0]-evidenceLeadings, 0)
		for start > 0 && start < len(text) && (text[start]&0xC0) == 0x80 {
			start--
		}
		out = append(out, cutBytes(text[start:], evidenceBytes))
		if len(out) == maxEvidence {
			break
		}
	}
	return out
}
