package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

// The team channel's memory needs kmp-mcp: a node switched on without it
// does not start, rather than drop every write.
func TestTeamChannelMemoryNeedsKMP(t *testing.T) {
	var stdout, stderr bytes.Buffer
	missing := filepath.Join(t.TempDir(), "kmp-mcp")
	code := run([]string{"--control", "unix:///nonexistent.sock", "--state", t.TempDir(), "--team-channel-memory", "--kmp-mcp", missing}, &stdout, &stderr)
	if code != exitUsage || !strings.Contains(stderr.String(), "--team-channel-memory needs kmp-mcp") {
		t.Fatalf("%d %s", code, stderr.String())
	}
	t.Setenv("AXLR_NODE_TEAM_CHANNEL_MEMORY", "true")
	t.Setenv("AXLR_NODE_KMP_MCP", missing)
	stderr.Reset()
	if code := run([]string{"--control", "unix:///nonexistent.sock", "--state", t.TempDir()}, &stdout, &stderr); code != exitUsage || !strings.Contains(stderr.String(), "needs kmp-mcp") {
		t.Fatalf("from the environment: %d %s", code, stderr.String())
	}
}
