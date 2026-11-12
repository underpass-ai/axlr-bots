package unixapi

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"
)

// NewOperatorClient talks to the control plane's operator address, an
// https:// URL, with the TLS configuration (the fleet CA as RootCAs) and
// the bearer token on every request: what a command or an input adapter
// on another host uses in place of the socket. The dial and the
// handshake are bounded; a call is not, since a run can take as long as
// a task: the caller's context bounds it, as on the socket.
func NewOperatorClient(address string, config *tls.Config, token string) *Client {
	dialer := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 15 * time.Second}
	transport := &http.Transport{
		TLSClientConfig:     config,
		ForceAttemptHTTP2:   true,
		DialContext:         dialer.DialContext,
		TLSHandshakeTimeout: 10 * time.Second,
		IdleConnTimeout:     90 * time.Second,
	}
	base := strings.TrimRight(address, "/")
	return &Client{http: newLiveClient(&http.Client{Transport: transport}), base: base, address: base, token: token}
}

// Health asks the control plane's /healthz: nil when it answers ok.
func (c *Client) Health(ctx context.Context) error {
	var answer map[string]string
	if err := c.call(ctx, http.MethodGet, "/healthz", nil, &answer); err != nil {
		return err
	}
	if answer["status"] != "ok" {
		return fmt.Errorf("control plane at %s: /healthz answered %v", c.address, answer)
	}
	return nil
}
