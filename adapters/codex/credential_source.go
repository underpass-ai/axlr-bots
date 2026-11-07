package codex

import "context"

// CredentialSource hands out the login's credentials (AuthFile).
type CredentialSource interface {
	Credentials(ctx context.Context) (Credentials, error)
	Renew(ctx context.Context, refused string) (Credentials, error)
}
