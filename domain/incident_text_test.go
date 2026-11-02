package domain

import (
	"strings"
	"testing"
)

func TestIncidentTextFitsWhatValidationAccepts(t *testing.T) {
	cases := map[string]struct {
		raw, want string
		max       int
	}{
		"kept":            {raw: "  OOMKilled\n\tlimit 32Mi ", want: "OOMKilled\n\tlimit 32Mi", max: 100},
		"carriage return": {raw: "line one\r\nline two\x00\x1b", want: "line one\nline two", max: 100},
		"empty":           {raw: " \r\x07 ", want: incidentTextPlaceholder, max: 100},
		"cut":             {raw: "abcdefghij", want: "abcdef…", max: 9},
		"cut in a rune":   {raw: "ñññññ", want: "ññ…", max: 8},
	}
	for name, c := range cases {
		got := IncidentText(c.raw, c.max)
		if got != c.want {
			t.Errorf("%s: %q, want %q", name, got, c.want)
		}
		if _, err := validateText(name, got, c.max); err != nil {
			t.Errorf("%s: %q refused: %v", name, got, err)
		}
	}
	long := IncidentText(strings.Repeat("x", 20000), MaxIncidentDiagnosisLength)
	if len(long) > MaxIncidentDiagnosisLength || !strings.HasSuffix(long, "…") {
		t.Errorf("long: %d bytes, ends %q", len(long), long[len(long)-5:])
	}
}
