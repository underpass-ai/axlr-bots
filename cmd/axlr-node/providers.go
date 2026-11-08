package main

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"time"
)

// Environment the providers are read from: whether a key is set, never
// its value, and where the Codex proxy listens.
const (
	openRouterKeyEnv      = "OPENROUTER_API_KEY"
	codexProxyListenEnv   = "AXLR_CODEX_PROXY_LISTEN"
	defaultCodexListen    = "127.0.0.1:8790"
	codexProxyDialTimeout = 500 * time.Millisecond
)

// providerProbe finds the model providers this node's bots can reach, and
// names them for the node's capabilities: `openrouter` when the host has
// OpenRouter's key, `codex` when it has the Codex proxy's key or the proxy
// answers on its listen address, `jev` when the node has the jev runtime
// (the same test that turned it on: Jev's key), and `local:<id>` for each
// local_models entry of the AXLR settings the node's runs read. Only the
// names leave the host: no key, no variable's name, no address.
type providerProbe struct {
	// Env reads a variable; nil is os.Getenv.
	Env func(string) string
	// Dial reports whether something listens at the address; nil is a
	// short TCP dial.
	Dial func(address string) bool
	// Settings is AXLR's settings.json; empty is ~/.config/axlr/settings.json,
	// beside the mcp.json the node's runs read.
	Settings string
	// Runtimes are the runtimes the node has beside AXLR.
	Runtimes []string
	// Log takes a line when the settings cannot be read; nil is silent.
	Log func(format string, args ...any)
}

// providers names what was found, in a fixed order: openrouter, codex,
// jev, then the local models as the settings list them.
func (p providerProbe) providers() []string {
	var names []string
	if p.env(openRouterKeyEnv) != "" {
		names = append(names, "openrouter")
	}
	if p.env(codexProxyKeyEnv) != "" || p.dial(envOr(codexProxyListenEnv, defaultCodexListen)) {
		names = append(names, "codex")
	}
	for _, runtime := range p.Runtimes {
		if runtime == "jev" {
			names = append(names, "jev")
			break
		}
	}
	for _, id := range p.localModels() {
		names = append(names, "local:"+id)
	}
	return names
}

func (p providerProbe) env(name string) string {
	if p.Env != nil {
		return p.Env(name)
	}
	return os.Getenv(name)
}

func (p providerProbe) dial(address string) bool {
	if p.Dial != nil {
		return p.Dial(address)
	}
	conn, err := net.DialTimeout("tcp", address, codexProxyDialTimeout)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

// localModels are the ids of the settings' local_models, each once, in
// the file's order. A settings file that is missing names none; one
// that is not JSON is logged and names none.
func (p providerProbe) localModels() []string {
	path := p.Settings
	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil
		}
		path = filepath.Join(home, ".config", "axlr", "settings.json")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var settings struct {
		LocalModels []struct {
			ID string `json:"id"`
		} `json:"local_models"`
	}
	if err := json.Unmarshal(raw, &settings); err != nil {
		p.log("node: %s is not valid JSON: no local model is named (%v)", path, err)
		return nil
	}
	var ids []string
	seen := map[string]bool{}
	for _, model := range settings.LocalModels {
		if model.ID == "" || seen[model.ID] {
			continue
		}
		seen[model.ID] = true
		ids = append(ids, model.ID)
	}
	return ids
}

func (p providerProbe) log(format string, args ...any) {
	if p.Log != nil {
		p.Log(format, args...)
	}
}
