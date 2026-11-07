package codex

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// The OAuth server Codex refreshes its login against, and the client
// `codex login` is issued to.
const (
	DefaultTokenURL = "https://auth.openai.com/oauth/token"
	DefaultClientID = "app_EMoamEEZ73f0CkXaXp7hrann"
)

// DefaultRefreshBefore is how long before the access token expires the
// login is refreshed. A token lives about ten days; Codex refreshes after
// eight.
const DefaultRefreshBefore = 48 * time.Hour

const (
	maxAuthFileBytes    = 1 << 20
	maxRefreshBytes     = 64 << 10
	refreshTimeout      = 30 * time.Second
	refreshScope        = "openid profile email"
	refreshGrantType    = "refresh_token"
	authFileMode        = 0o600
	authFileTempPattern = ".auth.json.axlr-*"
)

// AuthFile is the ChatGPT login of the host's user, auth.json under
// CODEX_HOME as `codex login` writes it. It is read on every request, so
// a login Codex refreshed or a new `codex login` is seen at once; when
// the access token is about to expire it is refreshed with the refresh
// token and written back as Codex writes it, keeping every other field.
type AuthFile struct {
	Path string
	// TokenURL is the OAuth token endpoint; empty uses DefaultTokenURL.
	TokenURL   string
	HTTPClient *http.Client
	// RefreshBefore is how long before expiry to refresh; zero uses
	// DefaultRefreshBefore.
	RefreshBefore time.Duration
	// Now is the clock; nil uses time.Now.
	Now func() time.Time

	mu sync.Mutex
}

// Credentials returns the login's tokens, refreshed first when the access
// token expires within RefreshBefore.
func (a *AuthFile) Credentials(ctx context.Context) (Credentials, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	doc, err := a.read()
	if err != nil {
		return Credentials{}, err
	}
	expiry := doc.expiry()
	if expiry.IsZero() || expiry.After(a.now().Add(a.refreshBefore())) {
		return doc.credentials(), nil
	}
	return a.refresh(ctx, doc)
}

// Renew answers a token the backend refused: the file's tokens when
// another process (Codex itself) has already replaced the refused one,
// otherwise a refresh.
func (a *AuthFile) Renew(ctx context.Context, refused string) (Credentials, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	doc, err := a.read()
	if err != nil {
		return Credentials{}, err
	}
	if doc.token("access_token") != refused {
		return doc.credentials(), nil
	}
	return a.refresh(ctx, doc)
}

func (a *AuthFile) read() (authDocument, error) {
	file, err := os.Open(a.Path)
	if err != nil {
		return authDocument{}, fmt.Errorf("the Codex login: %w (run codex login)", err)
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxAuthFileBytes+1))
	if err != nil {
		return authDocument{}, fmt.Errorf("the Codex login: %w", err)
	}
	if len(data) > maxAuthFileBytes {
		return authDocument{}, errors.New("the Codex login: auth.json is over 1 MiB")
	}
	doc, err := parseAuthDocument(data)
	if err != nil {
		return authDocument{}, fmt.Errorf("the Codex login: %w", err)
	}
	return doc, nil
}

// refresh trades the refresh token for new tokens and writes them back.
// The server rotates the refresh token, so a Codex that refreshed first
// leaves this one refused: the file is read again, and its tokens are
// taken when they are not the stale ones.
func (a *AuthFile) refresh(ctx context.Context, doc authDocument) (Credentials, error) {
	answer, err := a.exchange(ctx, doc)
	if err != nil {
		if again, readErr := a.read(); readErr == nil && again.token("refresh_token") != doc.token("refresh_token") {
			return again.credentials(), nil
		}
		return Credentials{}, err
	}
	data, updated, err := doc.refreshed(answer, a.now())
	if err != nil {
		return Credentials{}, err
	}
	if err := a.write(data); err != nil {
		return Credentials{}, fmt.Errorf("the Codex login: writing the refreshed tokens: %w", err)
	}
	return updated.credentials(), nil
}

func (a *AuthFile) exchange(ctx context.Context, doc authDocument) (tokenResponse, error) {
	body, _ := json.Marshal(map[string]string{
		"client_id":     doc.clientID(),
		"grant_type":    refreshGrantType,
		"refresh_token": doc.token("refresh_token"),
		"scope":         refreshScope,
	})
	ctx, cancel := context.WithTimeout(ctx, refreshTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, a.tokenURL(), bytes.NewReader(body))
	if err != nil {
		return tokenResponse{}, err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	response, err := a.client().Do(request)
	if err != nil {
		return tokenResponse{}, fmt.Errorf("the token refresh: %w", err)
	}
	defer response.Body.Close()
	var answer tokenResponse
	data, _ := io.ReadAll(io.LimitReader(response.Body, maxRefreshBytes))
	decodeErr := json.Unmarshal(data, &answer)
	if response.StatusCode/100 != 2 {
		return tokenResponse{}, answer.failure(response.StatusCode)
	}
	if decodeErr != nil || answer.AccessToken == "" {
		return tokenResponse{}, errors.New("the token refresh answered without an access token")
	}
	return answer, nil
}

// write replaces the file in one rename, mode 0600, so Codex never reads
// half a login.
func (a *AuthFile) write(data []byte) error {
	temp, err := os.CreateTemp(filepath.Dir(a.Path), authFileTempPattern)
	if err != nil {
		return err
	}
	defer os.Remove(temp.Name())
	if err := temp.Chmod(authFileMode); err != nil {
		temp.Close()
		return err
	}
	if _, err := temp.Write(data); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(temp.Name(), a.Path)
}

func (a *AuthFile) now() time.Time {
	if a.Now != nil {
		return a.Now()
	}
	return time.Now()
}

func (a *AuthFile) refreshBefore() time.Duration {
	if a.RefreshBefore > 0 {
		return a.RefreshBefore
	}
	return DefaultRefreshBefore
}

func (a *AuthFile) tokenURL() string {
	if a.TokenURL != "" {
		return a.TokenURL
	}
	return DefaultTokenURL
}

func (a *AuthFile) client() *http.Client {
	if a.HTTPClient != nil {
		return a.HTTPClient
	}
	return http.DefaultClient
}
