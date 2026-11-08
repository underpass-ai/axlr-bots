package main

import (
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func listenLoopback(t *testing.T) net.Listener {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	return listener
}

// The probe names what the host has, in a fixed order, and never the key
// itself, the variable or the address.
func TestProviderProbeNamesWhatTheHostHas(t *testing.T) {
	dir := t.TempDir()
	settings := filepath.Join(dir, "settings.json")
	_ = os.WriteFile(settings, []byte(`{"theme":"dark","local_models":[
		{"id":"codex/gpt-6.1-sol","url":"http://127.0.0.1:8790/v1","api_key_env":"AXLR_CODEX_PROXY_KEY"},
		{"id":"codex/gpt-6-luna"},{"id":""},{"id":"codex/gpt-6-luna"}]}`), 0o600)
	env := map[string]string{openRouterKeyEnv: "sk-or-secret", codexProxyKeyEnv: "proxy-secret"}
	lookup := func(name string) string { return env[name] }
	dialed := ""
	probe := providerProbe{Env: lookup, Dial: func(address string) bool { dialed = address; return false }, Settings: settings, Runtimes: []string{"jev"}}
	got := probe.providers()
	want := []string{"openrouter", "codex", "jev", "local:codex/gpt-6.1-sol", "local:codex/gpt-6-luna"}
	if !slices.Equal(got, want) {
		t.Fatalf("providers = %v, want %v", got, want)
	}
	if dialed != "" {
		t.Fatal("the proxy was dialed although its key is set")
	}
	for _, name := range got {
		if strings.Contains(name, "secret") || strings.Contains(name, "KEY") || strings.Contains(name, "8790") {
			t.Fatalf("%q leaks the host's environment", name)
		}
	}

	// Without the proxy's key, codex is there when the proxy answers.
	delete(env, codexProxyKeyEnv)
	env[codexProxyListenEnv] = "127.0.0.1:1"
	t.Setenv(codexProxyListenEnv, "127.0.0.1:1")
	probe.Dial = func(address string) bool { dialed = address; return address == "127.0.0.1:1" }
	if got := probe.providers(); !slices.Equal(got, want) || dialed != "127.0.0.1:1" {
		t.Fatalf("with the proxy answering: %v (dialed %q)", got, dialed)
	}
	probe.Dial = func(string) bool { return false }
	probe.Runtimes = nil
	if got := probe.providers(); !slices.Equal(got, []string{"openrouter", "local:codex/gpt-6.1-sol", "local:codex/gpt-6-luna"}) {
		t.Fatalf("without the proxy or jev: %v", got)
	}

	// A bare host names nothing; a settings file that is not JSON is
	// logged and names no local model; a missing one is silent.
	var logged []string
	bare := providerProbe{Env: func(string) string { return "" }, Dial: func(string) bool { return false }, Settings: filepath.Join(dir, "none.json"), Log: func(format string, args ...any) { logged = append(logged, format) }}
	if got := bare.providers(); got != nil || len(logged) != 0 {
		t.Fatalf("bare host: %v %v", got, logged)
	}
	_ = os.WriteFile(filepath.Join(dir, "broken.json"), []byte("{"), 0o600)
	bare.Settings = filepath.Join(dir, "broken.json")
	if got := bare.providers(); got != nil || len(logged) != 1 {
		t.Fatalf("broken settings: %v %v", got, logged)
	}
}

// The defaults read the process environment, dial the proxy's address
// and look in the home's AXLR settings.
func TestProviderProbeDefaults(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(openRouterKeyEnv, "")
	t.Setenv(codexProxyKeyEnv, "")
	listener := listenLoopback(t)
	t.Setenv(codexProxyListenEnv, listener.Addr().String())
	_ = os.MkdirAll(filepath.Join(home, ".config", "axlr"), 0o755)
	_ = os.WriteFile(filepath.Join(home, ".config", "axlr", "settings.json"), []byte(`{"local_models":[{"id":"ollama/qwen"}]}`), 0o600)
	if got := (providerProbe{}).providers(); !slices.Equal(got, []string{"codex", "local:ollama/qwen"}) {
		t.Fatalf("defaults: %v", got)
	}
	listener.Close()
	t.Setenv(openRouterKeyEnv, "sk-or-x")
	if got := (providerProbe{}).providers(); !slices.Equal(got, []string{"openrouter", "local:ollama/qwen"}) {
		t.Fatalf("after the proxy stopped: %v", got)
	}
}
