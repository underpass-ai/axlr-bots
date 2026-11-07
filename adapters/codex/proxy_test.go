package codex

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeLogin hands out a token and, on a renewal, the next one.
type fakeLogin struct {
	tokens  []string
	renewed []string
	err     error
}

func (f *fakeLogin) Credentials(context.Context) (Credentials, error) {
	if f.err != nil {
		return Credentials{}, f.err
	}
	return Credentials{AccessToken: f.tokens[0], AccountID: "acct-1"}, nil
}

func (f *fakeLogin) Renew(_ context.Context, refused string) (Credentials, error) {
	f.renewed = append(f.renewed, refused)
	f.tokens = f.tokens[1:]
	if len(f.tokens) == 0 {
		return Credentials{}, errors.New("no login left")
	}
	return Credentials{AccessToken: f.tokens[0]}, nil
}

func post(t *testing.T, handler http.Handler, path, authorization, body string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "text/event-stream")
	if authorization != "" {
		request.Header.Set("Authorization", authorization)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

// A request goes to the backend's /responses with the login's token and
// account instead of the caller's key, and the stream comes back with the
// backend's headers but not its cookies.
func TestTheProxyForwardsWithTheLogin(t *testing.T) {
	var seen *http.Request
	var seenBody string
	backend := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		seen = r
		data, _ := io.ReadAll(r.Body)
		seenBody = string(data)
		rw.Header().Set("Content-Type", "text/event-stream")
		rw.Header().Set("X-Codex-Primary-Used-Percent", "3")
		rw.Header().Set("Set-Cookie", "session=x")
		_, _ = rw.Write([]byte("data: {\"type\":\"response.completed\"}\n\n"))
	}))
	defer backend.Close()
	proxy := &Proxy{Auth: &fakeLogin{tokens: []string{"tok-1"}}, Upstream: backend.URL + "/", Key: "node-key", Logf: t.Logf}
	recorder := post(t, proxy, "/v1/responses", "Bearer node-key", `{"model":"gpt-6-luna"}`)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), "response.completed") {
		t.Fatalf("answer = %d %q", recorder.Code, recorder.Body.String())
	}
	if seen.URL.Path != "/responses" || seen.Header.Get("Authorization") != "Bearer tok-1" || seen.Header.Get("chatgpt-account-id") != "acct-1" {
		t.Errorf("upstream request = %s %v", seen.URL.Path, seen.Header)
	}
	if seen.Header.Get("Accept") != "text/event-stream" || seen.Header.Get("Content-Type") != "application/json" || seenBody != `{"model":"gpt-6-luna"}` {
		t.Errorf("upstream request lost its body or headers: %v %q", seen.Header, seenBody)
	}
	if recorder.Header().Get("X-Codex-Primary-Used-Percent") != "3" || recorder.Header().Get("Set-Cookie") != "" {
		t.Errorf("answer headers = %v", recorder.Header())
	}
}

func TestARefusedTokenIsRenewedOnce(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok-2" {
			rw.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = rw.Write([]byte("ok"))
	}))
	defer backend.Close()
	login := &fakeLogin{tokens: []string{"tok-1", "tok-2"}}
	recorder := post(t, &Proxy{Auth: login, Upstream: backend.URL}, "/responses", "", "{}")
	if recorder.Code != http.StatusOK || len(login.renewed) != 1 || login.renewed[0] != "tok-1" {
		t.Fatalf("answer = %d, renewed %v", recorder.Code, login.renewed)
	}
	login = &fakeLogin{tokens: []string{"tok-0", "tok-1"}}
	if recorder := post(t, &Proxy{Auth: login, Upstream: backend.URL}, "/responses", "", "{}"); recorder.Code != http.StatusUnauthorized || len(login.renewed) != 1 {
		t.Fatalf("a second refusal = %d, renewed %v", recorder.Code, login.renewed)
	}
	login = &fakeLogin{tokens: []string{"tok-0"}}
	if recorder := post(t, &Proxy{Auth: login, Upstream: backend.URL}, "/responses", "", "{}"); recorder.Code != http.StatusBadGateway {
		t.Fatalf("a failed renewal = %d", recorder.Code)
	}
}

func TestTheProxyRefusesWhatItDoesNotServe(t *testing.T) {
	proxy := &Proxy{Auth: &fakeLogin{tokens: []string{"t"}}, Key: "node-key", Upstream: "http://127.0.0.1:1"}
	if recorder := post(t, proxy, "/v1/chat/completions", "Bearer node-key", "{}"); recorder.Code != http.StatusNotFound {
		t.Errorf("another path = %d", recorder.Code)
	}
	recorder := httptest.NewRecorder()
	proxy.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/v1/responses", nil))
	if recorder.Code != http.StatusMethodNotAllowed || recorder.Header().Get("Allow") != http.MethodPost {
		t.Errorf("a GET = %d %v", recorder.Code, recorder.Header())
	}
	for _, authorization := range []string{"", "Bearer other", "node-key-but-longer"} {
		if recorder := post(t, proxy, "/v1/responses", authorization, "{}"); recorder.Code != http.StatusUnauthorized || !strings.Contains(recorder.Body.String(), "invalid_api_key") {
			t.Errorf("key %q = %d %s", authorization, recorder.Code, recorder.Body.String())
		}
	}
	if recorder := post(t, proxy, "/v1/responses", "Bearer node-key", strings.Repeat("x", maxRequestBytes+1)); recorder.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("a large request = %d", recorder.Code)
	}
	if recorder := post(t, proxy, "/v1/responses", "Bearer node-key", "{}"); recorder.Code != http.StatusBadGateway {
		t.Errorf("an unreachable backend = %d", recorder.Code)
	}
	proxy.Auth = &fakeLogin{err: errors.New("the Codex login: run codex login")}
	if recorder := post(t, proxy, "/v1/responses", "Bearer node-key", "{}"); recorder.Code != http.StatusBadGateway || !strings.Contains(recorder.Body.String(), "codex login") {
		t.Errorf("no login = %d %s", recorder.Code, recorder.Body.String())
	}
}

func TestTheDefaultsAreCodexs(t *testing.T) {
	proxy := &Proxy{}
	if proxy.upstream() != DefaultUpstream || proxy.client() != http.DefaultClient {
		t.Fatal("defaults changed")
	}
	auth := &AuthFile{}
	if auth.tokenURL() != DefaultTokenURL || auth.refreshBefore() != DefaultRefreshBefore || auth.client() != http.DefaultClient || auth.now().IsZero() {
		t.Fatal("defaults changed")
	}
}
