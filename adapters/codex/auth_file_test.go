package codex

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

var now = time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)

// jwt is an unsigned token whose payload carries the claims.
func jwt(claims map[string]any) string {
	payload, _ := json.Marshal(claims)
	return "e30." + base64.RawURLEncoding.EncodeToString(payload) + ".sig"
}

func accessToken(expires time.Time) string {
	return jwt(map[string]any{"exp": expires.Unix(), "client_id": "app_test"})
}

// writeLogin writes auth.json as `codex login` does, with a field the
// proxy does not know, which a refresh must keep.
func writeLogin(t *testing.T, dir, access, refresh string) string {
	t.Helper()
	path := filepath.Join(dir, "auth.json")
	doc := map[string]any{
		"OPENAI_API_KEY": nil,
		"auth_mode":      "chatgpt",
		"last_refresh":   "2026-11-01T00:00:00Z",
		"future_field":   map[string]any{"kept": true},
		"tokens":         map[string]any{"access_token": access, "refresh_token": refresh, "id_token": "id-old", "account_id": "acct-1"},
	}
	data, _ := json.Marshal(doc)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func readLogin(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	return doc
}

func TestAFreshLoginIsHandedOutAsItIs(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) }))
	defer server.Close()
	access := accessToken(now.Add(7 * 24 * time.Hour))
	auth := &AuthFile{Path: writeLogin(t, t.TempDir(), access, "refresh-1"), TokenURL: server.URL, Now: func() time.Time { return now }}
	credentials, err := auth.Credentials(context.Background())
	if err != nil || credentials.AccessToken != access || credentials.AccountID != "acct-1" {
		t.Fatalf("credentials = %+v, %v", credentials, err)
	}
	if calls.Load() != 0 {
		t.Fatalf("a fresh login was refreshed")
	}
}

// A token about to expire is traded as Codex trades it, and the file
// keeps every field it had, with the new tokens and last_refresh.
func TestAnExpiringLoginIsRefreshedAndWrittenBack(t *testing.T) {
	newAccess := accessToken(now.Add(10 * 24 * time.Hour))
	var asked map[string]string
	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/json" {
			rw.WriteHeader(http.StatusBadRequest)
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&asked)
		fmt.Fprintf(rw, `{"access_token": %q, "refresh_token": "refresh-2", "id_token": "id-new"}`, newAccess)
	}))
	defer server.Close()
	path := writeLogin(t, t.TempDir(), accessToken(now.Add(time.Hour)), "refresh-1")
	auth := &AuthFile{Path: path, TokenURL: server.URL, Now: func() time.Time { return now }}
	credentials, err := auth.Credentials(context.Background())
	if err != nil || credentials.AccessToken != newAccess || credentials.AccountID != "acct-1" {
		t.Fatalf("credentials = %+v, %v", credentials, err)
	}
	want := map[string]string{"client_id": "app_test", "grant_type": "refresh_token", "refresh_token": "refresh-1", "scope": "openid profile email"}
	for key, value := range want {
		if asked[key] != value {
			t.Errorf("refresh %s = %q, want %q", key, asked[key], value)
		}
	}
	doc := readLogin(t, path)
	tokens := doc["tokens"].(map[string]any)
	if tokens["access_token"] != newAccess || tokens["refresh_token"] != "refresh-2" || tokens["id_token"] != "id-new" || tokens["account_id"] != "acct-1" {
		t.Errorf("tokens = %v", tokens)
	}
	if doc["auth_mode"] != "chatgpt" || doc["future_field"] == nil || doc["last_refresh"] != now.Format(time.RFC3339Nano) {
		t.Errorf("auth.json = %v", doc)
	}
	if _, present := doc["OPENAI_API_KEY"]; !present {
		t.Errorf("the null API key was dropped")
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v", info.Mode())
	}
	if leftovers, _ := filepath.Glob(filepath.Join(filepath.Dir(path), ".auth.json.axlr-*")); len(leftovers) != 0 {
		t.Errorf("temporary files left: %v", leftovers)
	}
}

// A refresh token Codex rotated first is refused; the file then holds
// Codex's new login, which is taken.
func TestARefusedRefreshTakesTheLoginCodexWrote(t *testing.T) {
	dir := t.TempDir()
	codexAccess := accessToken(now.Add(10 * 24 * time.Hour))
	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
		writeLogin(t, dir, codexAccess, "refresh-codex")
		rw.WriteHeader(http.StatusUnauthorized)
		_, _ = rw.Write([]byte(`{"error": {"code": "refresh_token_reused"}}`))
	}))
	defer server.Close()
	auth := &AuthFile{Path: writeLogin(t, dir, accessToken(now.Add(-time.Hour)), "refresh-1"), TokenURL: server.URL, Now: func() time.Time { return now }}
	credentials, err := auth.Credentials(context.Background())
	if err != nil || credentials.AccessToken != codexAccess {
		t.Fatalf("credentials = %+v, %v", credentials, err)
	}
}

func TestARefusedRefreshSaysWhy(t *testing.T) {
	for body, want := range map[string]string{
		`{"error": {"code": "refresh_token_expired"}}`: "refresh_token_expired",
		`{"error": "invalid_grant"}`:                   "invalid_grant",
		`not json`:                                     "(401)",
	} {
		server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
			rw.WriteHeader(http.StatusUnauthorized)
			_, _ = rw.Write([]byte(body))
		}))
		auth := &AuthFile{Path: writeLogin(t, t.TempDir(), accessToken(now), "refresh-1"), TokenURL: server.URL, Now: func() time.Time { return now }}
		_, err := auth.Credentials(context.Background())
		server.Close()
		if err == nil || !strings.Contains(err.Error(), want) || strings.Contains(err.Error(), "refresh-1") {
			t.Errorf("%s: err = %v", body, err)
		}
	}
}

func TestARefreshWithoutAnAccessTokenFails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
		_, _ = rw.Write([]byte(`{"refresh_token": "r"}`))
	}))
	defer server.Close()
	path := writeLogin(t, t.TempDir(), accessToken(now), "refresh-1")
	auth := &AuthFile{Path: path, TokenURL: server.URL, Now: func() time.Time { return now }}
	if _, err := auth.Credentials(context.Background()); err == nil {
		t.Fatal("an answer without an access token was taken")
	}
	if readLogin(t, path)["tokens"].(map[string]any)["refresh_token"] != "refresh-1" {
		t.Fatal("the file changed after a failed refresh")
	}
	auth.TokenURL = "http://127.0.0.1:1"
	if _, err := auth.Credentials(context.Background()); err == nil {
		t.Fatal("an unreachable token endpoint passed")
	}
}

// A token the backend refused is renewed: by the file when Codex already
// replaced it, by a refresh otherwise.
func TestRenewTakesANewerLoginOrRefreshes(t *testing.T) {
	var calls atomic.Int32
	refreshed := accessToken(now.Add(10 * 24 * time.Hour))
	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		fmt.Fprintf(rw, `{"access_token": %q}`, refreshed)
	}))
	defer server.Close()
	current := accessToken(now.Add(5 * 24 * time.Hour))
	auth := &AuthFile{Path: writeLogin(t, t.TempDir(), current, "refresh-1"), TokenURL: server.URL, Now: func() time.Time { return now }}
	credentials, err := auth.Renew(context.Background(), "an-older-token")
	if err != nil || credentials.AccessToken != current || calls.Load() != 0 {
		t.Fatalf("renew of a replaced token = %+v, %v, %d refreshes", credentials, err, calls.Load())
	}
	credentials, err = auth.Renew(context.Background(), current)
	if err != nil || credentials.AccessToken != refreshed || calls.Load() != 1 {
		t.Fatalf("renew of the current token = %+v, %v, %d refreshes", credentials, err, calls.Load())
	}
	if readLogin(t, auth.Path)["tokens"].(map[string]any)["refresh_token"] != "refresh-1" {
		t.Error("a refresh that returned no refresh token dropped the old one")
	}
}

func TestATokenWithoutExpiryIsUsedAsItIs(t *testing.T) {
	auth := &AuthFile{Path: writeLogin(t, t.TempDir(), "opaque-token", "refresh-1"), TokenURL: "http://127.0.0.1:1"}
	credentials, err := auth.Credentials(context.Background())
	if err != nil || credentials.AccessToken != "opaque-token" {
		t.Fatalf("credentials = %+v, %v", credentials, err)
	}
	doc, _ := parseAuthDocument([]byte(`{"tokens": {"access_token": "opaque", "refresh_token": "r"}}`))
	if doc.clientID() != DefaultClientID {
		t.Errorf("client = %q", doc.clientID())
	}
}

func TestALoginTheProxyCannotUseIsRefused(t *testing.T) {
	dir := t.TempDir()
	for name, content := range map[string]string{
		"not-json":   `[`,
		"api-key":    `{"auth_mode": "apikey", "OPENAI_API_KEY": "sk"}`,
		"no-tokens":  `{"auth_mode": "chatgpt"}`,
		"no-refresh": `{"tokens": {"access_token": "a"}}`,
		"too-large":  `{"x": "` + strings.Repeat("x", maxAuthFileBytes) + `"}`,
	} {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := (&AuthFile{Path: path}).Credentials(context.Background()); err == nil {
			t.Errorf("%s: accepted", name)
		}
		if _, err := (&AuthFile{Path: path}).Renew(context.Background(), "a"); err == nil {
			t.Errorf("%s: renewed", name)
		}
	}
	if _, err := (&AuthFile{Path: filepath.Join(dir, "missing")}).Credentials(context.Background()); err == nil || !strings.Contains(err.Error(), "codex login") {
		t.Errorf("a missing login: %v", err)
	}
}

func TestAWriteThatCannotHappenFailsTheRefresh(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
		_, _ = rw.Write([]byte(`{"access_token": "new"}`))
	}))
	defer server.Close()
	dir := t.TempDir()
	auth := &AuthFile{Path: writeLogin(t, dir, accessToken(now), "refresh-1"), TokenURL: server.URL, Now: func() time.Time { return now }}
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(dir, 0o700)
	if _, err := auth.Credentials(context.Background()); err == nil && os.Geteuid() != 0 {
		t.Fatal("a refresh that could not be written passed")
	}
}
