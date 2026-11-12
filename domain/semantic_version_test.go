package domain

import (
	"errors"
	"strings"
	"testing"
)

func TestParseSemanticVersionTakesSemverOnly(t *testing.T) {
	for _, good := range []string{"0.0.0", "1.4.0", "10.20.30", "2.0.0-rc.1", "1.0.0-alpha", "1.0.0-0.3.7", "1.0.0-x.7.z.92", "1.4.0+build.7", "1.0.0-beta+exp.sha.5114f85", "1.0.0+20130313144700"} {
		version, err := ParseSemanticVersion("version", good)
		if err != nil || version.String() != good || version.IsZero() {
			t.Errorf("%q: %v %q", good, err, version)
		}
	}
	for _, bad := range []string{"", "1", "1.2", "v1.2.3", "01.2.3", "1.02.3", "1.2.03", "1.2.3-", "1.2.3+", "1.2.3-01", "1.2.3-a..b", "1.2.3 ", " 1.2.3", "latest", "1.2.3.4", "1.2.3-" + strings.Repeat("a", 64)} {
		if _, err := ParseSemanticVersion("version", bad); err == nil {
			t.Errorf("%q was taken", bad)
		} else if !strings.Contains(err.Error(), "version") || !errors.As(err, new(ValidationError)) {
			t.Errorf("%q: %v", bad, err)
		}
	}
	if !SemanticVersion("").IsZero() {
		t.Error("the zero value is no version")
	}
}
