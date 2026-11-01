package jev

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// MCPConfig finds a program's MCP server by its name in the node's AXLR
// configuration, ~/.config/axlr/mcp.json: the same file the node's AXLR
// reads, so a server is named once per node. A plugin
// entry names a manifest; the manifest gives the server's URL.
type MCPConfig struct {
	// Path is the mcp.json; empty means $HOME/.config/axlr/mcp.json.
	Path string
}

// Resolve answers the URL of the server named.
func (c MCPConfig) Resolve(name string) (string, error) {
	path := c.Path
	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("the node's home is unknown: %w", err)
		}
		path = filepath.Join(home, ".config", "axlr", "mcp.json")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("the node's MCP configuration: %w", err)
	}
	var config struct {
		Plugins []struct {
			Name     string `json:"name"`
			Manifest string `json:"manifest"`
			URL      string `json:"url"`
		} `json:"plugins"`
	}
	if err := json.Unmarshal(raw, &config); err != nil {
		return "", fmt.Errorf("%s is not valid JSON: %w", path, err)
	}
	for _, plugin := range config.Plugins {
		if plugin.Name != name {
			continue
		}
		address := plugin.URL
		if address == "" && plugin.Manifest != "" {
			manifest := plugin.Manifest
			if !filepath.IsAbs(manifest) {
				manifest = filepath.Join(filepath.Dir(path), manifest)
			}
			raw, err := os.ReadFile(manifest)
			if err != nil {
				return "", fmt.Errorf("the manifest of MCP server %s: %w", name, err)
			}
			var entry struct {
				URL string `json:"url"`
			}
			if err := json.Unmarshal(raw, &entry); err != nil {
				return "", fmt.Errorf("the manifest of MCP server %s is not valid JSON: %w", name, err)
			}
			address = entry.URL
		}
		parsed, err := url.Parse(address)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
			return "", fmt.Errorf("MCP server %s has no http(s) URL in %s", name, path)
		}
		return strings.TrimRight(address, "/"), nil
	}
	return "", errors.New("the node's MCP configuration names no server " + name)
}
