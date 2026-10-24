package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallAndUninstallRenderIntoADestDir(t *testing.T) {
	dest := t.TempDir()
	t.Setenv("HOME", "/home/ana")
	t.Setenv("OPENROUTER_API_KEY", "sk-or-test")
	var stdout, stderr bytes.Buffer
	nodeBinary := filepath.Join(t.TempDir(), "underpass-node")
	if err := os.WriteFile(nodeBinary, []byte("#!/bin/sh\necho node\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Without --node-binary the node binary must sit beside this one.
	if code := run([]string{"install", "--destdir", dest, "--no-systemctl"}, &stdout, &stderr); code != exitError || !strings.Contains(stderr.String(), "node binary") {
		t.Errorf("missing node binary: %d %s", code, stderr.String())
	}
	stderr.Reset()
	code := run([]string{"install", "--destdir", dest, "--no-systemctl", "--axlr-bin", "/opt/axlr-tui", "--node-binary", nodeBinary}, &stdout, &stderr)
	if code != exitOK {
		t.Fatalf("install: %d %s", code, stderr.String())
	}
	var report map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report["mode"] != "user" || report["binary"] != "/home/ana/.local/bin/underpass-bots" || report["nodeBinary"] != "/home/ana/.local/bin/underpass-node" || report["commands"] != nil {
		t.Errorf("report = %v", report)
	}
	service, err := os.ReadFile(filepath.Join(dest, "home/ana/.config/systemd/user/underpass-bots.service"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(service), "Type=simple\n") || !strings.Contains(string(service), "ExecStart=/home/ana/.local/bin/underpass-bots --state /home/ana/.local/state/underpass-bots") || strings.Contains(string(service), "sk-or") {
		t.Errorf("service:\n%s", service)
	}
	env, err := os.ReadFile(filepath.Join(dest, "home/ana/.config/underpass-bots/env"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(env), "OPENROUTER_API_KEY=sk-or-test\n") || !strings.Contains(string(env), "UNDERPASS_AXLR_BIN=/opt/axlr-tui\n") {
		t.Errorf("env:\n%s", env)
	}
	if strings.Contains(stdout.String(), "sk-or-test") {
		t.Error("the key was printed")
	}
	// The installed binary is this test binary, byte for byte.
	self, _ := os.Executable()
	want, _ := os.Stat(self)
	got, err := os.Stat(filepath.Join(dest, "home/ana/.local/bin/underpass-bots"))
	if err != nil || got.Size() != want.Size() || got.Mode().Perm() != 0o755 {
		t.Errorf("binary: %v", err)
	}

	if node, err := os.ReadFile(filepath.Join(dest, "home/ana/.local/bin/underpass-node")); err != nil || !strings.Contains(string(node), "echo node") {
		t.Errorf("node binary: %v", err)
	}
	if unit, err := os.ReadFile(filepath.Join(dest, "home/ana/.config/systemd/user/underpass-node.service")); err != nil || !strings.Contains(string(unit), "--control unix:///home/ana/.local/state/underpass-bots/underpass-bots.sock") {
		t.Errorf("node unit: %v", err)
	}
	// The system variant with explicit paths, into the same dest.
	stdout.Reset()
	code = run([]string{"install", "--system", "--destdir", dest, "--no-systemctl", "--prefix", "/opt/ub", "--state", "/srv/ub", "--node-state", "/srv/ubn", "--unit-dir", "/etc/units", "--env-file", "/etc/ub/env", "--node-binary", nodeBinary}, &stdout, &stderr)
	if code != exitOK {
		t.Fatalf("system install: %d %s", code, stderr.String())
	}
	service, _ = os.ReadFile(filepath.Join(dest, "etc/units/underpass-bots.service"))
	if !strings.Contains(string(service), "ProtectSystem=strict\n") || !strings.Contains(string(service), "ReadWritePaths=/srv/ub\n") || !strings.Contains(string(service), "ExecStart=/opt/ub/bin/underpass-bots --state /srv/ub ") {
		t.Errorf("system service:\n%s", service)
	}
	if node, _ := os.ReadFile(filepath.Join(dest, "etc/units/underpass-node.service")); !strings.Contains(string(node), "--state /srv/ubn\n") {
		t.Errorf("system node unit:\n%s", node)
	}
	if _, err := os.Stat(filepath.Join(dest, "etc/ub/env")); err != nil {
		t.Error(err)
	}

	stdout.Reset()
	code = run([]string{"uninstall", "--destdir", dest, "--no-systemctl", "--purge"}, &stdout, &stderr)
	if code != exitOK {
		t.Fatalf("uninstall: %d %s", code, stderr.String())
	}
	for _, gone := range []string{"home/ana/.local/bin/underpass-bots", "home/ana/.local/bin/underpass-node", "home/ana/.config/systemd/user/underpass-bots.service", "home/ana/.config/systemd/user/underpass-node.service", "home/ana/.config/underpass-bots/env", "home/ana/.local/state/underpass-bots", "home/ana/.local/state/underpass-node"} {
		if _, err := os.Stat(filepath.Join(dest, gone)); err == nil {
			t.Errorf("%s still there", gone)
		}
	}
	// A failure on disk is reported: the unit dir is a file.
	broken := t.TempDir()
	if err := os.MkdirAll(filepath.Join(broken, "home/ana/.config/systemd"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(broken, "home/ana/.config/systemd/user"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code := run([]string{"install", "--destdir", broken, "--no-systemctl", "--node-binary", nodeBinary}, &stdout, &stderr); code != exitError {
		t.Errorf("broken install: %d", code)
	}
	if code := run([]string{"uninstall", "--destdir", broken, "--no-systemctl"}, &stdout, &stderr); code != exitError {
		t.Errorf("broken uninstall: %d", code)
	}
}
