package codex

// Credentials are what a request to Codex's backend carries: the login's
// bearer token and the ChatGPT account it acts for.
type Credentials struct {
	AccessToken string
	AccountID   string
}
