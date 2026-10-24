package nodeproto

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// tokenPrefix marks a token so a pasted value is checked before it is
// decoded.
const tokenPrefix = "axlr1."

// EnrollToken is what the operator carries from the control plane to a
// new host: the token's id, its secret, the node id the certificate will
// name and the CA the node must trust when it first connects. It is one
// line, URL-safe, and nothing else needs copying.
type EnrollToken struct {
	ID     string `json:"id"`
	Secret string `json:"secret"`
	NodeID string `json:"nodeId"`
	CAPEM  string `json:"ca"`
}

// Encode renders the token as one line.
func (t EnrollToken) Encode() string {
	raw, _ := json.Marshal(t)
	return tokenPrefix + base64.RawURLEncoding.EncodeToString(raw)
}

// DecodeEnrollToken reads a token an operator pasted.
func DecodeEnrollToken(encoded string) (EnrollToken, error) {
	encoded = strings.TrimSpace(encoded)
	if !strings.HasPrefix(encoded, tokenPrefix) {
		return EnrollToken{}, errors.New("not an enrollment token (it starts with " + tokenPrefix + ")")
	}
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(encoded, tokenPrefix))
	if err != nil {
		return EnrollToken{}, fmt.Errorf("enrollment token: %w", err)
	}
	var token EnrollToken
	if err := json.Unmarshal(raw, &token); err != nil {
		return EnrollToken{}, fmt.Errorf("enrollment token: %w", err)
	}
	if token.ID == "" || token.Secret == "" || token.NodeID == "" {
		return EnrollToken{}, errors.New("enrollment token: incomplete")
	}
	return token, nil
}
