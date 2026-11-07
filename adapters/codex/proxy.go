// Package codex lets a node's bots run on the ChatGPT subscription of
// the host's user instead of an API key. The proxy serves the OpenAI
// Responses API on a loopback address; AXLR names it as a local_models
// entry with api "responses", and the proxy sends each request on to
// Codex's backend with the login `codex login` stored in auth.json, which
// it refreshes as Codex does. AXLR's requests are unchanged: the backend
// takes them as they are, tool calls and encrypted reasoning included.
package codex

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"
)

// DefaultUpstream is Codex's backend for a ChatGPT login; the proxy posts
// to its /responses.
const DefaultUpstream = "https://chatgpt.com/backend-api/codex"

const (
	maxRequestBytes = 32 << 20
	copyBufferBytes = 32 << 10
	accountHeader   = "chatgpt-account-id"
	userAgent       = "axlr-node-codex-proxy"
)

// Proxy is the loopback Responses endpoint: POST /responses or
// /v1/responses.
type Proxy struct {
	Auth CredentialSource
	// Upstream is the backend's base URL; empty uses DefaultUpstream.
	Upstream string
	// Key, when set, is the bearer token a request must carry, so that
	// only the node's runs spend the subscription.
	Key string
	// Client sends the requests; it must not time a whole stream out.
	Client *http.Client
	// Logf reports each request's outcome; nil logs nothing.
	Logf func(format string, args ...any)
}

func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/responses" && r.URL.Path != "/v1/responses" {
		writeError(w, http.StatusNotFound, "not_found", "the proxy serves POST /v1/responses only")
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeError(w, http.StatusMethodNotAllowed, "invalid_request_error", "the proxy serves POST /v1/responses only")
		return
	}
	if !p.authorized(r) {
		writeError(w, http.StatusUnauthorized, "invalid_api_key", "the proxy's key is missing or wrong")
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxRequestBytes+1))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request_error", "the request body could not be read")
		return
	}
	if len(body) > maxRequestBytes {
		writeError(w, http.StatusRequestEntityTooLarge, "invalid_request_error", "the request is over 32 MiB")
		return
	}
	started := time.Now()
	ctx := r.Context()
	credentials, err := p.Auth.Credentials(ctx)
	if err != nil {
		p.logf("codex-proxy: %v", err)
		writeError(w, http.StatusBadGateway, "authentication_error", err.Error())
		return
	}
	response, err := p.send(ctx, r, body, credentials)
	if err == nil && response.StatusCode == http.StatusUnauthorized {
		response.Body.Close()
		p.logf("codex-proxy: the backend refused the token; renewing the login")
		if credentials, err = p.Auth.Renew(ctx, credentials.AccessToken); err == nil {
			response, err = p.send(ctx, r, body, credentials)
		}
	}
	if err != nil {
		p.logf("codex-proxy: %v", err)
		writeError(w, http.StatusBadGateway, "server_error", err.Error())
		return
	}
	defer response.Body.Close()
	copyHeaders(w.Header(), response.Header)
	w.WriteHeader(response.StatusCode)
	stream(w, response.Body)
	p.logf("codex-proxy: %s %d in %s", r.URL.Path, response.StatusCode, time.Since(started).Round(time.Millisecond))
}

func (p *Proxy) send(ctx context.Context, r *http.Request, body []byte, credentials Credentials) (*http.Response, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, p.upstream()+"/responses", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	for _, name := range []string{"Content-Type", "Accept", "OpenAI-Beta"} {
		if value := r.Header.Get(name); value != "" {
			request.Header.Set(name, value)
		}
	}
	request.Header.Set("Authorization", "Bearer "+credentials.AccessToken)
	if credentials.AccountID != "" {
		request.Header.Set(accountHeader, credentials.AccountID)
	}
	request.Header.Set("User-Agent", userAgent)
	return p.client().Do(request)
}

func (p *Proxy) authorized(r *http.Request) bool {
	if p.Key == "" {
		return true
	}
	given := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	return subtle.ConstantTimeCompare([]byte(given), []byte(p.Key)) == 1
}

func (p *Proxy) upstream() string {
	if p.Upstream != "" {
		return strings.TrimRight(p.Upstream, "/")
	}
	return DefaultUpstream
}

func (p *Proxy) client() *http.Client {
	if p.Client != nil {
		return p.Client
	}
	return http.DefaultClient
}

func (p *Proxy) logf(format string, args ...any) {
	if p.Logf != nil {
		p.Logf(format, args...)
	}
}

// copyHeaders passes the backend's headers on, its rate limits included,
// but not the ones about this hop or its cookies.
func copyHeaders(to, from http.Header) {
	for name, values := range from {
		switch http.CanonicalHeaderKey(name) {
		case "Connection", "Keep-Alive", "Transfer-Encoding", "Content-Length", "Content-Encoding", "Set-Cookie", "Alt-Svc", "Strict-Transport-Security":
			continue
		}
		to[name] = values
	}
}

// stream copies the body as it arrives, flushing each piece, so the
// server-sent events reach AXLR without waiting for the end.
func stream(w http.ResponseWriter, body io.Reader) {
	flusher, _ := w.(http.Flusher)
	buffer := make([]byte, copyBufferBytes)
	for {
		n, err := body.Read(buffer)
		if n > 0 {
			if _, writeErr := w.Write(buffer[:n]); writeErr != nil {
				return
			}
			if flusher != nil {
				flusher.Flush()
			}
		}
		if err != nil {
			return
		}
	}
}

// writeError answers in OpenAI's error shape, which AXLR reads.
func writeError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"code": code, "type": code, "message": message}})
}
