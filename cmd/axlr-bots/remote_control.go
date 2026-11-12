package main

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/underpass-ai/axlr-bots/adapters/unixapi"
)

// remoteControl is a control plane on another host, reached through its
// operator address (serve --api-listen) instead of the socket: the URL,
// the file holding the operator token, and the CA its certificate is
// checked against (the fleet CA, axlr-bots ca show). With a URL every
// command that would use the socket uses it, and none falls back to the
// state directory: a control plane that does not answer is an error.
type remoteControl struct {
	URL       string
	TokenFile string
	CAFile    string
}

// enabled reports whether a URL was given.
func (r remoteControl) enabled() bool {
	return r.URL != ""
}

// validate checks the flags go together before any command runs.
func (r remoteControl) validate() error {
	if !r.enabled() {
		if r.TokenFile != "" || r.CAFile != "" {
			return errors.New("--token-file and --ca-file go with --control")
		}
		return nil
	}
	parsed, err := url.Parse(r.URL)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || (parsed.Path != "" && parsed.Path != "/") || parsed.RawQuery != "" {
		return fmt.Errorf("--control %s: give the operator address as https://host:port", r.URL)
	}
	if r.TokenFile == "" {
		return errors.New("--control needs --token-file (or AXLR_BOTS_TOKEN_FILE), the operator token")
	}
	return nil
}

// client reads the token and the CA and returns the operator client.
func (r remoteControl) client() (*unixapi.Client, error) {
	raw, err := os.ReadFile(r.TokenFile)
	if err != nil {
		return nil, fmt.Errorf("--token-file: %w", err)
	}
	token := strings.TrimSpace(string(raw))
	if token == "" {
		return nil, fmt.Errorf("--token-file %s is empty", r.TokenFile)
	}
	config := &tls.Config{MinVersion: tls.VersionTLS13}
	if r.CAFile != "" {
		pem, err := os.ReadFile(r.CAFile)
		if err != nil {
			return nil, fmt.Errorf("--ca-file: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("--ca-file %s holds no certificate", r.CAFile)
		}
		config.RootCAs = pool
	}
	return unixapi.NewOperatorClient(r.URL, config, token), nil
}

// where names the control plane a command reaches, for the adapters' logs.
func (r remoteControl) where(socketPath string) string {
	if r.enabled() {
		return r.URL
	}
	return socketPath
}

// localOnly reports whether the command works on the local state
// directory whatever --control says: the daemon itself, the fleet CA,
// and the reconciliation of a stopped daemon's tasks.
func localOnly(rest []string) bool {
	if len(rest) == 0 {
		return false
	}
	switch rest[0] {
	case "serve", "ca", "install", "uninstall":
		return true
	}
	return len(rest) > 1 && rest[0] == "task" && rest[1] == "reconcile"
}

// clientOr is the operator client with --control, else the socket's.
func (r remoteControl) clientOr(socketPath string) (*unixapi.Client, error) {
	if r.enabled() {
		return r.client()
	}
	return unixapi.NewClient(socketPath), nil
}
