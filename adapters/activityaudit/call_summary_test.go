package activityaudit

import "testing"

func TestReadCallSummary(t *testing.T) {
	for name, c := range map[string]struct {
		summary string
		want    callSummary
	}{
		"whole":          {localExec("/home/gx10a/bin/arreglo", "plan", "x"), callSummary{Program: "arreglo", Arg: "plan", ArgKnown: true}},
		"whole, no args": {localExec("/usr/bin/true"), callSummary{Program: "true", ArgKnown: true}},
		"why first, cut": {cutSummary, callSummary{}},
		"program, cut args": {`{"why":"w","program":"/x/qa-probe","args":["cli","--sandbox…`,
			callSummary{Program: "qa-probe", Arg: "cli", ArgKnown: true}},
		"program, empty args, cut": {`{"why":"w","program":"/x/qa-probe","args":[],"cwd":"…`, callSummary{Program: "qa-probe", ArgKnown: true}},
		"program, args cut inside": {`{"why":"w","program":"/x/qa-probe","args":["cl…`, callSummary{Program: "qa-probe"}},
		"empty":                    {"", callSummary{}},
	} {
		if got := readCallSummary(c.summary); got != c.want {
			t.Errorf("%s: %+v, want %+v", name, got, c.want)
		}
	}
}
