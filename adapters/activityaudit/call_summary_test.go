package activityaudit

import "testing"

func TestReadCallSummary(t *testing.T) {
	for name, c := range map[string]struct {
		summary string
		want    callSummary
	}{
		"whole":          {localExec("/home/user-a/bin/fixer", "plan", "x"), callSummary{Program: "fixer", Arg: "plan", ArgKnown: true}},
		"whole, no args": {localExec("/usr/bin/true"), callSummary{Program: "true", ArgKnown: true}},
		"why first, cut": {cutSummary, callSummary{}},
		"program, cut args": {`{"why":"w","program":"/x/sandbox-probe","args":["cli","--sandbox…`,
			callSummary{Program: "sandbox-probe", Arg: "cli", ArgKnown: true}},
		"program, empty args, cut": {`{"why":"w","program":"/x/sandbox-probe","args":[],"cwd":"…`, callSummary{Program: "sandbox-probe", ArgKnown: true}},
		"program, args cut inside": {`{"why":"w","program":"/x/sandbox-probe","args":["cl…`, callSummary{Program: "sandbox-probe"}},
		"empty":                    {"", callSummary{}},
	} {
		if got := readCallSummary(c.summary); got != c.want {
			t.Errorf("%s: %+v, want %+v", name, got, c.want)
		}
	}
}
