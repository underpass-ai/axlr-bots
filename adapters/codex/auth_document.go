package codex

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// authDocument is auth.json as `codex login` writes it. Every field is
// kept as it was read, so writing the refreshed tokens back changes the
// tokens and last_refresh and nothing Codex may add later.
type authDocument struct {
	fields map[string]json.RawMessage
	tokens map[string]json.RawMessage
}

func parseAuthDocument(data []byte) (authDocument, error) {
	var doc authDocument
	if err := json.Unmarshal(data, &doc.fields); err != nil {
		return authDocument{}, errors.New("auth.json is not a JSON object")
	}
	var mode string
	_ = json.Unmarshal(doc.fields["auth_mode"], &mode)
	if mode != "" && mode != "chatgpt" {
		return authDocument{}, errors.New("auth.json holds a " + mode + " login, not a ChatGPT one: run codex login")
	}
	if err := json.Unmarshal(doc.fields["tokens"], &doc.tokens); err != nil || doc.tokens == nil {
		return authDocument{}, errors.New("auth.json holds no ChatGPT tokens: run codex login")
	}
	if doc.token("access_token") == "" || doc.token("refresh_token") == "" {
		return authDocument{}, errors.New("auth.json lacks the access or the refresh token: run codex login")
	}
	return doc, nil
}

func (d authDocument) token(name string) string {
	var value string
	_ = json.Unmarshal(d.tokens[name], &value)
	return value
}

func (d authDocument) credentials() Credentials {
	return Credentials{AccessToken: d.token("access_token"), AccountID: d.token("account_id")}
}

// expiry is when the access token stops being accepted, read from its
// exp claim; the zero time when the token says nothing.
func (d authDocument) expiry() time.Time {
	var claims struct {
		Exp int64 `json:"exp"`
	}
	if claim(d.token("access_token"), &claims) != nil || claims.Exp == 0 {
		return time.Time{}
	}
	return time.Unix(claims.Exp, 0)
}

// clientID is the OAuth client the login was issued to, which a refresh
// names; Codex's own when the token does not say.
func (d authDocument) clientID() string {
	var claims struct {
		ClientID string `json:"client_id"`
	}
	if claim(d.token("access_token"), &claims) != nil || claims.ClientID == "" {
		return DefaultClientID
	}
	return claims.ClientID
}

// refreshed is the document with the tokens a refresh returned; a token
// the answer leaves out stays as it was.
func (d authDocument) refreshed(answer tokenResponse, now time.Time) ([]byte, authDocument, error) {
	tokens := make(map[string]json.RawMessage, len(d.tokens))
	for name, value := range d.tokens {
		tokens[name] = value
	}
	for name, value := range map[string]string{"access_token": answer.AccessToken, "refresh_token": answer.RefreshToken, "id_token": answer.IDToken} {
		if value != "" {
			tokens[name], _ = json.Marshal(value)
		}
	}
	fields := make(map[string]json.RawMessage, len(d.fields))
	for name, value := range d.fields {
		fields[name] = value
	}
	fields["tokens"], _ = json.Marshal(tokens)
	fields["last_refresh"], _ = json.Marshal(now.UTC().Format(time.RFC3339Nano))
	data, err := json.MarshalIndent(fields, "", "  ")
	if err != nil {
		return nil, authDocument{}, err
	}
	return data, authDocument{fields: fields, tokens: tokens}, nil
}

// claim decodes the payload of a JWT without checking its signature:
// the token is only read for its expiry and client, never trusted.
func claim(token string, into any) error {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return errors.New("not a JWT")
	}
	payload, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(parts[1], "="))
	if err != nil {
		return err
	}
	return json.Unmarshal(payload, into)
}
