package domain

import (
	"strings"
	"testing"
)

func TestQuestionIsCompleteAndShort(t *testing.T) {
	q, err := NewQuestion("  What breaks when the importer reads a file of 2 GB?\n\tSince v0.16.0.  ")
	if err != nil || q.String() != "What breaks when the importer reads a file of 2 GB?\n\tSince v0.16.0." {
		t.Fatalf("question = %q %v", q, err)
	}
	// Characters, not bytes: 2000 accented letters are a question.
	if _, err := NewQuestion(strings.Repeat("é", MaxQuestionLength)); err != nil {
		t.Errorf("2000 characters: %v", err)
	}
	for _, bad := range []string{"", "   ", strings.Repeat("a", MaxQuestionLength+1), "a\x00b", "a\x1bb", "a\x7fb", "a\xffb"} {
		if _, err := NewQuestion(bad); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
}
