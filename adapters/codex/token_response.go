package codex

import (
	"encoding/json"
	"fmt"
)

// tokenResponse is the OAuth server's answer to a refresh: the new
// tokens, or an error whose code says why (refresh_token_expired,
// refresh_token_reused, refresh_token_invalidated).
type tokenResponse struct {
	AccessToken  string          `json:"access_token"`
	RefreshToken string          `json:"refresh_token"`
	IDToken      string          `json:"id_token"`
	Error        json.RawMessage `json:"error"`
}

// code is the error's code, which the server sends as a string or as an
// object with a code.
func (r tokenResponse) code() string {
	var text string
	if json.Unmarshal(r.Error, &text) == nil {
		return text
	}
	var object struct {
		Code string `json:"code"`
	}
	if json.Unmarshal(r.Error, &object) == nil {
		return object.Code
	}
	return ""
}

func (r tokenResponse) failure(status int) error {
	if code := r.code(); code != "" {
		return fmt.Errorf("the token refresh was refused (%d %s): run codex login", status, code)
	}
	return fmt.Errorf("the token refresh was refused (%d)", status)
}
