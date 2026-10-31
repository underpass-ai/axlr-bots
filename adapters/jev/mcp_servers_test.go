package jev

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A server is found by its plugin's name, through its manifest or its
// own url; a missing file, a bad URL or an unknown name say why.
func TestTheServerIsFoundInTheNodesMCPConfiguration(t *testing.T) {
	dir := t.TempDir()
	config := filepath.Join(dir, "mcp.json")
	_ = os.WriteFile(filepath.Join(dir, "score.json"), []byte(`{"id": "score", "url": "http://127.0.0.1:8790/mcp/ob-1/"}`), 0o600)
	_ = os.WriteFile(filepath.Join(dir, "bad.json"), []byte(`{"id": "bad", "url": "ftp://x"}`), 0o600)
	_ = os.WriteFile(config, []byte(`{"version": 1, "plugins": [
		{"manifest": "score.json", "name": "score"},
		{"manifest": "bad.json", "name": "bad"},
		{"name": "direct", "url": "https://world.example/mcp"},
		{"manifest": "missing.json", "name": "missing"}]}`), 0o600)
	servers := MCPConfig{Path: config}
	if url, err := servers.Resolve("score"); err != nil || url != "http://127.0.0.1:8790/mcp/ob-1" {
		t.Errorf("score = %q %v", url, err)
	}
	if url, err := servers.Resolve("direct"); err != nil || url != "https://world.example/mcp" {
		t.Errorf("direct = %q %v", url, err)
	}
	for name, want := range map[string]string{"bad": "no http(s) URL", "missing": "manifest of MCP server missing", "none": "names no server none"} {
		if _, err := servers.Resolve(name); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := (MCPConfig{Path: filepath.Join(dir, "nowhere.json")}).Resolve("score"); err == nil {
		t.Error("a missing configuration resolved")
	}
	_ = os.WriteFile(config, []byte(`{`), 0o600)
	if _, err := servers.Resolve("score"); err == nil || !strings.Contains(err.Error(), "not valid JSON") {
		t.Errorf("bad json: %v", err)
	}
	t.Setenv("HOME", dir)
	if _, err := (MCPConfig{}).Resolve("score"); err == nil || !strings.Contains(err.Error(), filepath.Join(dir, ".config", "axlr", "mcp.json")) {
		t.Errorf("default path: %v", err)
	}
}
