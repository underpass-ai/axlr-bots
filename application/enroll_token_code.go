package application

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

// EnrollTokenCode is what the operator carries from the control plane to a
// new host: the token's id, its secret, the node id the certificate will
// name and the CA the node must trust when it first connects. It is one
// line, URL-safe, and nothing else needs copying.
type EnrollTokenCode struct {
	ID     string `json:"id"`
	Secret string `json:"secret"`
	NodeID string `json:"nodeId"`
	CAPEM  string `json:"ca"`
}

// Encode renders the token as one line.
func (t EnrollTokenCode) Encode() string {
	raw, _ := json.Marshal(t)
	return tokenPrefix + base64.RawURLEncoding.EncodeToString(raw)
}

// DecodeEnrollTokenCode reads a token an operator pasted.
func DecodeEnrollTokenCode(encoded string) (EnrollTokenCode, error) {
	encoded = strings.TrimSpace(encoded)
	if !strings.HasPrefix(encoded, tokenPrefix) {
		return EnrollTokenCode{}, errors.New("not an enrollment token (it starts with " + tokenPrefix + ")")
	}
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(encoded, tokenPrefix))
	if err != nil {
		return EnrollTokenCode{}, fmt.Errorf("enrollment token: %w", err)
	}
	var token EnrollTokenCode
	if err := json.Unmarshal(raw, &token); err != nil {
		return EnrollTokenCode{}, fmt.Errorf("enrollment token: %w", err)
	}
	if token.ID == "" || token.Secret == "" || token.NodeID == "" {
		return EnrollTokenCode{}, errors.New("enrollment token: incomplete")
	}
	return token, nil
}
