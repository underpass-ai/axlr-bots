package activityaudit

import (
	"strings"
	"testing"
)

// What varies between two occurrences of one failure normalises away:
// ids, shas, times, paths under /home, numbers, quoted text.
func TestNormaliseTakesOutWhatVaries(t *testing.T) {
	a := Normalise(`task task-5dbfaeddfdb4f706d3a45963: head 1a2b3c4d failed at 2026-11-12T10:55:07.723Z reading /home/user-a/Documents/ai/x.json after 12 tries "fix the panel"`)
	b := Normalise(`task task-0dc89689b19a575f87602c94: head 9f8e7d6c failed at 2026-11-11T01:02:03Z reading /home/user-b/other/y.json after 3 tries "something else"`)
	if a != b {
		t.Fatalf("\n%s\n%s", a, b)
	}
	if want := `task <id>: head <hex> failed at <time> reading <path> after N tries "…"`; a != want {
		t.Errorf("got %q", a)
	}
	for in, want := range map[string]string{
		provider504: "interrupted: model endpoint <addr> provider_failure (HTTP 504)",
		`unknown registered plugin tool "axlr_remember"`:                    `unknown registered plugin tool "…"`,
		`unknown registered plugin tool "枠"`:                                `unknown registered plugin tool "…"`,
		"first line\nsecond line":                                           "first line",
		`escaped first\nsecond`:                                             "escaped first",
		"call call_cc22b74bf9b848869a14c41f54a8a7cc on localhost:8790 «hi»": "call <id> on <addr> «…»",
		"see https://example.com/a?b=1 and `x` and 'y' on 2026-11-12":       "see <url> and `…` and '…' on <date>",
		"uuid 72d3162e-cc78-11e3-81ab-4c9367dc0958, word deadbeef0":         "uuid <id>, word <hex>",
		"provider_failure (HTTP 429) after 3.5s":                            "provider_failure (HTTP 429) after Ns",
		"   ":                                                               "",
		"abcdefg is not hex-with-digit":                                     "abcdefg is not hex-with-digit",
	} {
		if got := Normalise(in); got != want {
			t.Errorf("Normalise(%q) = %q, want %q", in, got, want)
		}
	}
	if got := []rune(Normalise(strings.Repeat("word ", 200))); len(got) != MaxError || got[MaxError-1] != '…' {
		t.Errorf("not cut: %d", len(got))
	}
}

// A secret in an error never reaches the store or the event.
func TestASecretIsRedacted(t *testing.T) {
	for _, secret := range []string{"ghp_" + strings.Repeat("a", 36), "github_pat_" + strings.Repeat("b", 30), "sk-" + strings.Repeat("c", 24), "AKIAABCDEFGHIJKLMNOP", "xoxb-1234567890-abc", "-----BEGIN RSA PRIVATE KEY-----", "Bearer abcdefgh12345678"} {
		got := Normalise("auth failed with " + secret + " here")
		if strings.Contains(got, secret) || !strings.Contains(got, Redacted) {
			t.Errorf("%s: %q", secret, got)
		}
	}
}
