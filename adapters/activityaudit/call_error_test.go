package activityaudit

import "testing"

func TestReadCallError(t *testing.T) {
	for name, c := range map[string]struct {
		raw  string
		want callError
	}{
		"host refusal":       {rememberLine, callError{Text: "send the continuation alone to commit a reviewed write"}},
		"host refusal cut":   {`{"call_id":"call_x","error":"project:workers fix-details-ignore…`, callError{Text: "project:workers fix-details-ignore"}},
		"cut envelope":       {cutEnvelope, callError{Cut: true}},
		"cut bridged":        {bridgedCut, callError{Cut: true}},
		"whole envelope":     {execFailed("c1", 2, "error: no_credentials\nmore"), callError{Text: "error: no_credentials\nmore", Exit: "2"}},
		"error object":       {`{"error":{"code":"not_allowed","message":"path outside the workspace"}}`, callError{Text: "not_allowed: path outside the workspace"}},
		"error object bare":  {`{"error":{"message":"refused"}}`, callError{Text: "refused"}},
		"plain text":         {"permission denied", callError{Text: "permission denied"}},
		"empty":              {"", callError{Cut: true}},
		"cut with exit":      {`{"protocol_version":1,"status":"completed","output":{"exit_code":3,"stdout":"x…`, callError{Exit: "3", Cut: true}},
		"cut stderr":         {`{"output":{"exit_code":1,"stderr":"fatal: not a git repo…`, callError{Exit: "1", Text: "fatal: not a git repo"}},
		"cut other json":     {`{"weird":"thing…`, callError{Text: `{"weird":"thing`}},
		"cut broken escape":  {`{"error":"bad \`, callError{Text: "bad"}},
		"cut message":        {`{"status":"failed","message":"no such program…`, callError{Text: "no such program"}},
		"empty error string": {`{"error":"","output":{"stderr":"boom"}}`, callError{Text: "boom"}},
	} {
		if got := readCallError(c.raw); got != c.want {
			t.Errorf("%s: %+v, want %+v", name, got, c.want)
		}
	}
}
