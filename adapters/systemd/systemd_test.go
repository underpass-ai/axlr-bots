package systemd

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type recorder struct {
	calls []string
	fail  map[string]error
}

func (r *recorder) Run(name string, args ...string) error {
	line := name + " " + strings.Join(args, " ")
	r.calls = append(r.calls, line)
	for prefix, err := range r.fail {
		if strings.HasPrefix(line, prefix) {
			return err
		}
	}
	return nil
}

func sources(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	cp := filepath.Join(dir, "axlr-bots")
	node := filepath.Join(dir, "axlr-node")
	for _, path := range []string{cp, node} {
		if err := os.WriteFile(path, []byte("#!/bin/sh\necho "+filepath.Base(path)+"\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return cp, node
}

func mustRead(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestDefaultLayouts(t *testing.T) {
	usr := DefaultLayout(false, "", "/home/ana")
	if usr.System || usr.Prefix != "/home/ana/.local" || usr.StateDir != "/home/ana/.local/state/axlr-bots" || usr.NodeStateDir != "/home/ana/.local/state/axlr-node" || usr.UnitDir != "/home/ana/.config/systemd/user" || usr.EnvFile != "/home/ana/.config/axlr-bots/env" {
		t.Errorf("user layout = %+v", usr)
	}
	if usr.BinaryPath() != "/home/ana/.local/bin/axlr-bots" || usr.NodeBinaryPath() != "/home/ana/.local/bin/axlr-node" || usr.SocketPath() != "/home/ana/.local/state/axlr-bots/axlr-bots.sock" || usr.NodeUnitPath() != "/home/ana/.config/systemd/user/axlr-node.service" {
		t.Error("user paths wrong")
	}
	if strings.Join(usr.Systemctl(), " ") != "systemctl --user" || usr.OnDisk("/x") != "/x" {
		t.Error("user systemctl or OnDisk wrong")
	}
	sys := DefaultLayout(true, "", "/home/ana")
	if !sys.System || sys.Prefix != "/usr/local" || sys.StateDir != "/var/lib/axlr-bots" || sys.NodeStateDir != "/var/lib/axlr-node" || sys.UnitDir != "/etc/systemd/system" || sys.EnvFile != "/etc/axlr-bots/env" || sys.ServicePath() != "/etc/systemd/system/axlr-bots.service" {
		t.Errorf("system layout = %+v", sys)
	}
	if strings.Join(sys.Systemctl(), " ") != "systemctl" {
		t.Error("system systemctl wrong")
	}
	sys.DestDir = "/tmp/dest"
	if sys.OnDisk("/etc/x") != "/tmp/dest/etc/x" {
		t.Error("OnDisk wrong")
	}
	if DefaultLayout(true, "/opt/ub", "").Prefix != "/opt/ub" || DefaultLayout(false, "/opt/ub", "/h").Prefix != "/opt/ub" {
		t.Error("explicit prefix ignored")
	}
}

func TestRenderUserUnits(t *testing.T) {
	l := DefaultLayout(false, "", "/home/ana")
	l.AXLRBin = "/opt/axlr-tui"
	service := RenderService(l)
	for _, want := range []string{
		"Description=AXLR Bots control plane\n", "Type=simple\n", "EnvironmentFile=-/home/ana/.config/axlr-bots/env\n",
		"ExecStart=/home/ana/.local/bin/axlr-bots --state /home/ana/.local/state/axlr-bots --socket /home/ana/.local/state/axlr-bots/axlr-bots.sock serve\n",
		"Restart=on-failure\n", "NoNewPrivileges=yes\n", "WantedBy=default.target\n", "not available to a user service",
	} {
		if !strings.Contains(service, want) {
			t.Errorf("service lacks %q:\n%s", want, service)
		}
	}
	for _, absent := range []string{"User=", "ProtectSystem=strict", "ProtectHome=yes", "StateDirectory=", "Environment=HOME", "After=", "OPENROUTER", "sk-or"} {
		if strings.Contains(service, absent) {
			t.Errorf("user service has %q", absent)
		}
	}
	node := RenderNodeUnit(l)
	for _, want := range []string{
		"Description=AXLR Bots node agent\n", "After=axlr-bots.service\nWants=axlr-bots.service\n", "Environment=AXLR_NODE_LABELS=host=%l\n",
		"ExecStart=/home/ana/.local/bin/axlr-node --control unix:///home/ana/.local/state/axlr-bots/axlr-bots.sock --state /home/ana/.local/state/axlr-node\n",
		"WantedBy=default.target\n",
	} {
		if !strings.Contains(node, want) {
			t.Errorf("node unit lacks %q:\n%s", want, node)
		}
	}
	if strings.Contains(node, "User=") || strings.Contains(node, "ProtectSystem=strict") {
		t.Errorf("user node unit:\n%s", node)
	}
	env := RenderEnv(l, "")
	if !strings.Contains(env, "#OPENROUTER_API_KEY=") || !strings.Contains(env, "AXLR_TUI=/opt/axlr-tui\n") {
		t.Errorf("env:\n%s", env)
	}
	if env := RenderEnv(l, "sk-or-test"); !strings.Contains(env, "\nOPENROUTER_API_KEY=sk-or-test\n") {
		t.Errorf("env with key:\n%s", env)
	}
}

func TestRenderSystemUnits(t *testing.T) {
	l := DefaultLayout(true, "", "")
	service := RenderService(l)
	for _, want := range []string{
		"After=network-online.target\n", "User=axlr-bots\n", "Group=axlr-bots\n", "SupplementaryGroups=systemd-journal\n",
		"Environment=HOME=/var/lib/axlr-bots\n", "ProtectSystem=strict\n", "ProtectHome=yes\n", "StateDirectory=axlr-bots\n",
		"ReadWritePaths=/var/lib/axlr-bots\n", "WantedBy=multi-user.target\n",
	} {
		if !strings.Contains(service, want) {
			t.Errorf("system service lacks %q:\n%s", want, service)
		}
	}
	node := RenderNodeUnit(l)
	for _, want := range []string{"User=axlr-bots\n", "Environment=HOME=/var/lib/axlr-node\n", "StateDirectory=axlr-node\n", "ReadWritePaths=/var/lib/axlr-node\n", "ReadWritePaths=/var/lib/axlr-bots\n", "ProtectSystem=strict\n"} {
		if !strings.Contains(node, want) {
			t.Errorf("system node unit lacks %q:\n%s", want, node)
		}
	}
	custom := l
	custom.StateDir = "/srv/ub"
	if s := RenderService(custom); strings.Contains(s, "StateDirectory=") || !strings.Contains(s, "ReadWritePaths=/srv/ub\n") {
		t.Errorf("custom state dir:\n%s", s)
	}
}

func TestInstallAndUninstallUserLayoutUnderADestDir(t *testing.T) {
	dest := t.TempDir()
	l := DefaultLayout(false, "", "/home/ana")
	l.DestDir = dest
	l.AXLRBin = "/opt/axlr-tui"
	cp, node := sources(t)
	runner := &recorder{}
	installer := Installer{Layout: l, Source: cp, NodeSource: node, Key: "sk-or-test", Runner: runner}
	report, err := installer.Install()
	if err != nil {
		t.Fatal(err)
	}
	if report.Mode != "user" || report.Binary != "/home/ana/.local/bin/axlr-bots" || report.NodeBinary != "/home/ana/.local/bin/axlr-node" || len(report.Units) != 2 || report.Socket != l.SocketPath() {
		t.Errorf("report = %+v", report)
	}
	for _, bin := range []string{"axlr-bots", "axlr-node"} {
		path := filepath.Join(dest, "home/ana/.local/bin", bin)
		if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o755 || !strings.Contains(mustRead(t, path), bin) {
			t.Errorf("binary %s: %v", bin, err)
		}
	}
	service := mustRead(t, filepath.Join(dest, "home/ana/.config/systemd/user/axlr-bots.service"))
	if service != RenderService(l) || strings.Contains(service, dest) {
		t.Errorf("service unit:\n%s", service)
	}
	if mustRead(t, filepath.Join(dest, "home/ana/.config/systemd/user/axlr-node.service")) != RenderNodeUnit(l) {
		t.Error("node unit differs")
	}
	envPath := filepath.Join(dest, "home/ana/.config/axlr-bots/env")
	if info, err := os.Stat(envPath); err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("env file: %v", err)
	}
	if env := mustRead(t, envPath); !strings.Contains(env, "OPENROUTER_API_KEY=sk-or-test\n") || !strings.Contains(env, "AXLR_TUI=/opt/axlr-tui\n") {
		t.Errorf("env:\n%s", env)
	}
	for _, dir := range []string{"home/ana/.local/state/axlr-bots", "home/ana/.local/state/axlr-node"} {
		if _, err := os.Stat(filepath.Join(dest, dir)); err != nil {
			t.Errorf("%s missing", dir)
		}
	}
	if strings.Join(runner.calls, "\n") != "systemctl --user daemon-reload" {
		t.Errorf("commands = %v", runner.calls)
	}
	steps := strings.Join(report.NextSteps, "\n")
	if !strings.Contains(steps, "systemctl --user enable --now axlr-bots.service axlr-node.service") || !strings.Contains(steps, "loginctl enable-linger") {
		t.Errorf("next steps = %v", report.NextSteps)
	}

	// A second install keeps the environment file (and its key).
	installer.Key = "sk-or-other"
	if _, err := installer.Install(); err != nil {
		t.Fatal(err)
	}
	if env := mustRead(t, envPath); !strings.Contains(env, "sk-or-test") || strings.Contains(env, "sk-or-other") {
		t.Error("the env file was overwritten")
	}

	runner.calls = nil
	removed, err := installer.Uninstall(false)
	if err != nil {
		t.Fatal(err)
	}
	if len(removed.Units) != 4 || len(removed.NextSteps) != 1 {
		t.Errorf("uninstall report = %+v", removed)
	}
	for _, gone := range []string{"home/ana/.local/bin/axlr-bots", "home/ana/.local/bin/axlr-node", "home/ana/.config/systemd/user/axlr-bots.service", "home/ana/.config/systemd/user/axlr-node.service"} {
		if _, err := os.Stat(filepath.Join(dest, gone)); err == nil {
			t.Errorf("%s still there", gone)
		}
	}
	if _, err := os.Stat(envPath); err != nil {
		t.Error("env file removed without --purge")
	}
	if strings.Join(runner.calls, "\n") != "systemctl --user disable --now axlr-node.service\nsystemctl --user disable --now axlr-bots.service\nsystemctl --user daemon-reload" {
		t.Errorf("commands = %v", runner.calls)
	}
	if _, err := installer.Uninstall(false); err != nil {
		t.Error(err)
	}
	purged, err := installer.Uninstall(true)
	if err != nil || len(purged.NextSteps) != 0 {
		t.Errorf("purge: %v %+v", err, purged)
	}
	for _, gone := range []string{envPath, filepath.Dir(envPath), filepath.Join(dest, "home/ana/.local/state/axlr-bots"), filepath.Join(dest, "home/ana/.local/state/axlr-node")} {
		if _, err := os.Stat(gone); err == nil {
			t.Errorf("%s kept after purge", gone)
		}
	}
}

func TestInstallSystemLayoutUnderADestDir(t *testing.T) {
	dest := t.TempDir()
	l := DefaultLayout(true, "", "")
	l.DestDir = dest
	cp, node := sources(t)
	runner := &recorder{}
	report, err := (Installer{Layout: l, Source: cp, NodeSource: node, Runner: runner, CreateUser: true}).Install()
	if err != nil {
		t.Fatal(err)
	}
	if report.Mode != "system" || report.EnvFile != "/etc/axlr-bots/env" {
		t.Errorf("report = %+v", report)
	}
	service := mustRead(t, filepath.Join(dest, "etc/systemd/system/axlr-bots.service"))
	if !strings.Contains(service, "ProtectSystem=strict\n") || !strings.Contains(service, "User=axlr-bots\n") || strings.Contains(service, dest) {
		t.Errorf("service:\n%s", service)
	}
	if env := mustRead(t, filepath.Join(dest, "etc/axlr-bots/env")); !strings.Contains(env, "#OPENROUTER_API_KEY=") {
		t.Errorf("env:\n%s", env)
	}
	for _, bin := range []string{"usr/local/bin/axlr-bots", "usr/local/bin/axlr-node"} {
		if _, err := os.Stat(filepath.Join(dest, bin)); err != nil {
			t.Error(err)
		}
	}
	calls := strings.Join(runner.calls, "\n")
	if !strings.Contains(calls, "useradd --system --home-dir /var/lib/axlr-bots --no-create-home --shell /usr/sbin/nologin --groups systemd-journal axlr-bots") || !strings.HasSuffix(calls, "systemctl daemon-reload") {
		t.Errorf("commands = %v", runner.calls)
	}
	joined := strings.Join(report.NextSteps, "\n")
	if !strings.Contains(joined, "chown axlr-bots: /var/lib/axlr-bots") || !strings.Contains(joined, "chown axlr-bots: /var/lib/axlr-node") || !strings.Contains(joined, "systemctl enable --now axlr-bots.service axlr-node.service") || strings.Contains(joined, "linger") {
		t.Errorf("next steps = %v", report.NextSteps)
	}
}

func TestInstallerFailures(t *testing.T) {
	l := DefaultLayout(false, "", "/home/ana")
	l.DestDir = t.TempDir()
	cp, node := sources(t)
	if _, err := (Installer{Layout: l, Source: cp}).Install(); err == nil {
		t.Error("no node source accepted")
	}
	if _, err := (Installer{Layout: l, Source: "/no/such/binary", NodeSource: node}).Install(); err == nil {
		t.Error("missing source accepted")
	}
	if _, err := (Installer{Layout: l, Source: cp, NodeSource: "/no/such/node"}).Install(); err == nil {
		t.Error("missing node source accepted")
	}
	runner := &recorder{fail: map[string]error{"systemctl --user daemon-reload": errors.New("no bus")}}
	if _, err := (Installer{Layout: l, Source: cp, NodeSource: node, Runner: runner}).Install(); err == nil || !strings.Contains(err.Error(), "no bus") {
		t.Errorf("daemon-reload failure: %v", err)
	}
	if _, err := (Installer{Layout: l, Source: cp, NodeSource: node, Runner: runner}).Uninstall(false); err == nil || !strings.Contains(err.Error(), "no bus") {
		t.Errorf("uninstall daemon-reload failure: %v", err)
	}
	sys := DefaultLayout(true, "", "")
	sys.DestDir = t.TempDir()
	failing := &recorder{fail: map[string]error{"useradd": errors.New("not root")}}
	if _, err := (Installer{Layout: sys, Source: cp, NodeSource: node, Runner: failing, CreateUser: true}).Install(); err == nil || !strings.Contains(err.Error(), "not root") {
		t.Errorf("useradd failure: %v", err)
	}
	blocked := DefaultLayout(false, "", "/home/ana")
	blocked.DestDir = t.TempDir()
	if err := os.MkdirAll(filepath.Dir(blocked.OnDisk(blocked.UnitDir)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(blocked.OnDisk(blocked.UnitDir), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := (Installer{Layout: blocked, Source: cp, NodeSource: node}).Install(); err == nil {
		t.Error("units written under a file")
	}
	if _, err := (Installer{Layout: blocked, Source: cp, NodeSource: node}).Uninstall(false); err == nil {
		t.Error("uninstall removed a unit under a file")
	}
	envDir := DefaultLayout(false, "", "/home/ana")
	envDir.DestDir = t.TempDir()
	if err := os.MkdirAll(envDir.OnDisk(envDir.EnvFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := (Installer{Layout: envDir, Source: cp, NodeSource: node}).Install(); err != nil {
		t.Errorf("an existing env path is kept: %v", err)
	}
	if currentUser() == "" {
		t.Error("currentUser empty")
	}
}

func TestExecRunner(t *testing.T) {
	if err := (ExecRunner{}).Run("sh", "-c", "exit 0"); err != nil {
		t.Error(err)
	}
	err := (ExecRunner{}).Run("sh", "-c", "echo nope >&2; exit 3")
	if err == nil || !strings.Contains(err.Error(), "nope") || !strings.Contains(err.Error(), "exit status 3") {
		t.Errorf("failure = %v", err)
	}
}
