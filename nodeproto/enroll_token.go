package nodeproto

import "github.com/underpass-ai/axlr-bots/application"

// EnrollToken is the one-line token an operator carries to a new host;
// the application defines it, the control plane's operator service mints
// it.
type EnrollToken = application.EnrollTokenCode

// DecodeEnrollToken reads a token an operator pasted.
func DecodeEnrollToken(encoded string) (EnrollToken, error) {
	return application.DecodeEnrollTokenCode(encoded)
}
