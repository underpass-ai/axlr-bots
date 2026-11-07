package main

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeCodexLogin(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	login := `{"auth_mode": "chatgpt", "tokens": {"access_token": "opaque", "refresh_token": "r", "account_id": "acct"}}`
	if err := os.WriteFile(filepath.Join(home, "auth.json"), []byte(login), 0o600); err != nil {
		t.Fatal(err)
	}
	return home
}

// The proxy serves on loopback with the login it read at start, takes
// only the node's key and stops with its context.
func TestCodexProxyServesUntilStopped(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer opaque" || r.Header.Get("chatgpt-account-id") != "acct" {
			rw.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = rw.Write([]byte("streamed"))
	}))
	defer backend.Close()
	t.Setenv(codexProxyKeyEnv, "node-key")
	ctx, cancel := context.WithCancel(context.Background())
	addresses := make(chan net.Addr, 1)
	done := make(chan int, 1)
	stderr := &syncBuffer{}
	go func() {
		done <- codexProxy(ctx, []string{"--listen", "127.0.0.1:0", "--codex-home", writeCodexLogin(t), "--upstream", backend.URL}, stderr, func(a net.Addr) { addresses <- a })
	}()
	var address net.Addr
	select {
	case address = <-addresses:
	case code := <-done:
		t.Fatalf("exited %d: %s", code, stderr.String())
	}
	request, _ := http.NewRequest(http.MethodPost, "http://"+address.String()+"/v1/responses", strings.NewReader("{}"))
	request.Header.Set("Authorization", "Bearer node-key")
	response, err := http.DefaultClient.Do(request)
	if err != nil || response.StatusCode != http.StatusOK {
		t.Fatalf("answer = %v, %v", response, err)
	}
	response.Body.Close()
	cancel()
	select {
	case code := <-done:
		if code != exitOK {
			t.Fatalf("exit = %d: %s", code, stderr.String())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the proxy did not stop")
	}
}

func TestCodexProxyRefusesWhatItCannotServe(t *testing.T) {
	home := writeCodexLogin(t)
	for _, c := range []struct {
		args []string
		code int
	}{
		{[]string{"--listen", "0.0.0.0:8790", "--codex-home", home}, exitUsage},
		{[]string{"--listen", "192.168.1.2:8790", "--codex-home", home}, exitUsage},
		{[]string{"--listen", "no-port", "--codex-home", home}, exitUsage},
		{[]string{"--codex-home", home, "extra"}, exitUsage},
		{[]string{"--no-such-flag"}, exitUsage},
		{[]string{"-h"}, exitOK},
		{[]string{"--listen", "127.0.0.1:0", "--codex-home", t.TempDir()}, exitError},
		{[]string{"--listen", "localhost:1", "--codex-home", home}, exitError},
	} {
		stderr := &syncBuffer{}
		if code := codexProxy(context.Background(), c.args, stderr, nil); code != c.code {
			t.Errorf("%v: exit %d, want %d: %s", c.args, code, c.code, stderr.String())
		}
	}
}

func TestCodexProxyIsASubcommand(t *testing.T) {
	stderr := &syncBuffer{}
	if code := run([]string{"codex-proxy", "--listen", "0.0.0.0:1"}, &syncBuffer{}, stderr); code != exitUsage || !strings.Contains(stderr.String(), "loopback") {
		t.Fatalf("exit %d: %s", code, stderr.String())
	}
	if defaultCodexHome() == "" {
		t.Fatal("no default Codex home")
	}
}
