package main

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"

	"github.com/underpass-ai/axlr-bots/adapters/grpcclient"
	"github.com/underpass-ai/axlr-bots/adapters/unixapi"
)

// remoteControl is a control plane on another host, reached through its
// operator address (serve --grpc-listen; with --transport rest,
// --api-listen) instead of the socket: the address, as host:port for
// gRPC or an https URL for REST,
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
func (r remoteControl) validate(transport string) error {
	if !r.enabled() {
		if r.TokenFile != "" || r.CAFile != "" {
			return errors.New("--token-file and --ca-file go with --control")
		}
		return nil
	}
	if transport != transportREST {
		if _, err := r.grpcAddress(); err != nil {
			return err
		}
		if r.TokenFile == "" {
			return errors.New("--control needs --token-file (or AXLR_BOTS_TOKEN_FILE), the operator token")
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

// grpcAddress is host:port of the gRPC operator address: --control as
// host:port, or with a https:// or grpcs:// scheme in front.
func (r remoteControl) grpcAddress() (string, error) {
	address := strings.TrimPrefix(strings.TrimPrefix(r.URL, "https://"), "grpcs://")
	address = strings.TrimSuffix(address, "/")
	host, port, err := net.SplitHostPort(address)
	if err != nil || host == "" || port == "" || strings.ContainsAny(address, "/?#") {
		return "", fmt.Errorf("--control %s: give the gRPC operator address as host:port", r.URL)
	}
	return address, nil
}

// credentials reads the operator token and builds the TLS configuration
// that checks the control plane's certificate against the fleet CA.
func (r remoteControl) credentials() (string, *tls.Config, error) {
	raw, err := os.ReadFile(r.TokenFile)
	if err != nil {
		return "", nil, fmt.Errorf("--token-file: %w", err)
	}
	token := strings.TrimSpace(string(raw))
	if token == "" {
		return "", nil, fmt.Errorf("--token-file %s is empty", r.TokenFile)
	}
	config := &tls.Config{MinVersion: tls.VersionTLS13}
	if r.CAFile != "" {
		pem, err := os.ReadFile(r.CAFile)
		if err != nil {
			return "", nil, fmt.Errorf("--ca-file: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pem) {
			return "", nil, fmt.Errorf("--ca-file %s holds no certificate", r.CAFile)
		}
		config.RootCAs = pool
	}
	return token, config, nil
}

// client reads the token and the CA and returns the HTTP operator client.
func (r remoteControl) client() (*unixapi.Client, error) {
	token, config, err := r.credentials()
	if err != nil {
		return nil, err
	}
	return unixapi.NewOperatorClient(r.URL, config, token), nil
}

// grpcClient reads the token and the CA and returns the gRPC operator
// client.
func (r remoteControl) grpcClient() (*grpcclient.Client, error) {
	address, err := r.grpcAddress()
	if err != nil {
		return nil, err
	}
	token, config, err := r.credentials()
	if err != nil {
		return nil, err
	}
	return grpcclient.DialTLS(address, config, token)
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
