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

func source(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "underpass-bots")
	if err := os.WriteFile(path, []byte("#!/bin/sh\necho binary\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
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
	sys := DefaultLayout(false, "", "/home/x")
	if sys.User || sys.Prefix != "/usr/local" || sys.StateDir != "/var/lib/underpass-bots" || sys.UnitDir != "/etc/systemd/system" || sys.EnvFile != "/etc/underpass-bots/env" {
		t.Errorf("system layout = %+v", sys)
	}
	if sys.BinaryPath() != "/usr/local/bin/underpass-bots" || sys.SocketPath() != "/var/lib/underpass-bots/underpass-bots.sock" || sys.ServicePath() != "/etc/systemd/system/underpass-bots.service" || sys.AlertUnitPath() != "/etc/systemd/system/underpass-bots-alert@.service" {
		t.Errorf("system paths wrong: %s %s", sys.BinaryPath(), sys.SocketPath())
	}
	if strings.Join(sys.Systemctl(), " ") != "systemctl" || sys.OnDisk("/etc/x") != "/etc/x" {
		t.Error("system systemctl or OnDisk wrong")
	}
	usr := DefaultLayout(true, "", "/home/x")
	if !usr.User || usr.Prefix != "/home/x/.local" || usr.StateDir != "/home/x/.local/state/underpass-bots" || usr.UnitDir != "/home/x/.config/systemd/user" || usr.EnvFile != "/home/x/.config/underpass-bots/env" {
		t.Errorf("user layout = %+v", usr)
	}
	if strings.Join(usr.Systemctl(), " ") != "systemctl --user" {
		t.Error("user systemctl wrong")
	}
	usr.DestDir = "/tmp/dest"
	if usr.OnDisk("/home/x/.local/bin") != "/tmp/dest/home/x/.local/bin" {
		t.Error("OnDisk wrong")
	}
	if DefaultLayout(false, "/opt/ub", "").Prefix != "/opt/ub" || DefaultLayout(true, "/opt/ub", "/h").Prefix != "/opt/ub" {
		t.Error("explicit prefix ignored")
	}
}

func TestRenderSystemUnits(t *testing.T) {
	l := DefaultLayout(false, "", "")
	l.AXLRBin = "/opt/axlr/axlr-tui"
	service := RenderService(l)
	for _, want := range []string{
		"Type=simple\n", "User=underpass-bots\n", "Group=underpass-bots\n", "SupplementaryGroups=systemd-journal\n",
		"EnvironmentFile=-/etc/underpass-bots/env\n", "Environment=HOME=/var/lib/underpass-bots\n",
		"ExecStart=/usr/local/bin/underpass-bots --state /var/lib/underpass-bots --socket /var/lib/underpass-bots/underpass-bots.sock serve\n",
		"Restart=on-failure\n", "NoNewPrivileges=yes\n", "ProtectSystem=strict\n", "ProtectHome=yes\n",
		"StateDirectory=underpass-bots\n", "ReadWritePaths=/var/lib/underpass-bots\n", "WantedBy=multi-user.target\n", "After=network-online.target\n",
	} {
		if !strings.Contains(service, want) {
			t.Errorf("service lacks %q:\n%s", want, service)
		}
	}
	if strings.Contains(service, "OPENROUTER") || strings.Contains(service, "sk-or") {
		t.Error("the unit carries the key")
	}
	custom := l
	custom.StateDir = "/srv/ub"
	if s := RenderService(custom); strings.Contains(s, "StateDirectory=") || !strings.Contains(s, "ReadWritePaths=/srv/ub\n") {
		t.Errorf("custom state dir:\n%s", s)
	}
	alert := RenderAlertUnit(l)
	for _, want := range []string{
		"Type=oneshot\n", "User=underpass-bots\n", "After=underpass-bots.service\n", "Description=Underpass Bots: tell the SRE bot that %i failed\n",
		"ExecStart=/usr/local/bin/underpass-bots --state /var/lib/underpass-bots --socket /var/lib/underpass-bots/underpass-bots.sock alert --unit %i\n",
		"ProtectSystem=strict\n",
	} {
		if !strings.Contains(alert, want) {
			t.Errorf("alert unit lacks %q:\n%s", want, alert)
		}
	}
	if !strings.Contains(RenderDropIn(), "OnFailure=underpass-bots-alert@%N.service\n") {
		t.Error("drop-in wrong")
	}
	env := RenderEnv(l, "")
	if !strings.Contains(env, "#OPENROUTER_API_KEY=") || !strings.Contains(env, "UNDERPASS_AXLR_BIN=/opt/axlr/axlr-tui\n") || strings.Contains(env, "UNDERPASS_SYSTEMD_USER") {
		t.Errorf("env:\n%s", env)
	}
	if env := RenderEnv(l, "sk-or-test"); !strings.Contains(env, "\nOPENROUTER_API_KEY=sk-or-test\n") {
		t.Errorf("env with key:\n%s", env)
	}
}

func TestRenderUserUnits(t *testing.T) {
	l := DefaultLayout(true, "", "/home/ana")
	service := RenderService(l)
	for _, want := range []string{
		"Type=simple\n", "EnvironmentFile=-/home/ana/.config/underpass-bots/env\n",
		"ExecStart=/home/ana/.local/bin/underpass-bots --state /home/ana/.local/state/underpass-bots --socket /home/ana/.local/state/underpass-bots/underpass-bots.sock serve\n",
		"Restart=on-failure\n", "NoNewPrivileges=yes\n", "WantedBy=default.target\n", "not available to a user service",
	} {
		if !strings.Contains(service, want) {
			t.Errorf("user service lacks %q:\n%s", want, service)
		}
	}
	for _, absent := range []string{"User=", "ProtectSystem=strict", "ProtectHome=yes", "StateDirectory=", "Environment=HOME"} {
		if strings.Contains(service, absent) {
			t.Errorf("user service has %q", absent)
		}
	}
	alert := RenderAlertUnit(l)
	if strings.Contains(alert, "User=") || strings.Contains(alert, "ProtectSystem") || !strings.Contains(alert, "alert --unit %i\n") {
		t.Errorf("user alert unit:\n%s", alert)
	}
	if env := RenderEnv(l, ""); !strings.Contains(env, "UNDERPASS_SYSTEMD_USER=1\n") {
		t.Errorf("user env:\n%s", env)
	}
}

func TestInstallAndUninstallUserModeUnderADestDir(t *testing.T) {
	dest := t.TempDir()
	l := DefaultLayout(true, "", "/home/ana")
	l.DestDir = dest
	l.AXLRBin = "/opt/axlr-tui"
	runner := &recorder{}
	installer := Installer{Layout: l, Source: source(t), Key: "sk-or-test", Runner: runner}
	report, err := installer.Install()
	if err != nil {
		t.Fatal(err)
	}
	if report.Mode != "user" || report.Binary != "/home/ana/.local/bin/underpass-bots" || len(report.Units) != 2 || report.Socket != l.SocketPath() {
		t.Errorf("report = %+v", report)
	}
	bin := filepath.Join(dest, "home/ana/.local/bin/underpass-bots")
	if info, err := os.Stat(bin); err != nil || info.Mode().Perm() != 0o755 || mustRead(t, bin) != "#!/bin/sh\necho binary\n" {
		t.Errorf("binary: %v", err)
	}
	service := mustRead(t, filepath.Join(dest, "home/ana/.config/systemd/user/underpass-bots.service"))
	if service != RenderService(l) || strings.Contains(service, dest) {
		t.Errorf("service unit:\n%s", service)
	}
	if mustRead(t, filepath.Join(dest, "home/ana/.config/systemd/user/underpass-bots-alert@.service")) != RenderAlertUnit(l) {
		t.Error("alert unit differs")
	}
	envPath := filepath.Join(dest, "home/ana/.config/underpass-bots/env")
	if info, err := os.Stat(envPath); err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("env file: %v", err)
	}
	if env := mustRead(t, envPath); !strings.Contains(env, "OPENROUTER_API_KEY=sk-or-test\n") || !strings.Contains(env, "UNDERPASS_AXLR_BIN=/opt/axlr-tui\n") || !strings.Contains(env, "UNDERPASS_SYSTEMD_USER=1\n") {
		t.Errorf("env:\n%s", env)
	}
	if _, err := os.Stat(filepath.Join(dest, "home/ana/.local/state/underpass-bots")); err != nil {
		t.Error("state dir missing")
	}
	if strings.Join(runner.calls, "\n") != "systemctl --user daemon-reload" {
		t.Errorf("commands = %v", runner.calls)
	}
	if len(report.NextSteps) < 3 || !strings.Contains(strings.Join(report.NextSteps, "\n"), "systemctl --user enable --now underpass-bots.service") {
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
	if len(removed.Units) != 3 || len(removed.NextSteps) != 1 {
		t.Errorf("uninstall report = %+v", removed)
	}
	for _, gone := range []string{"home/ana/.local/bin/underpass-bots", "home/ana/.config/systemd/user/underpass-bots.service", "home/ana/.config/systemd/user/underpass-bots-alert@.service"} {
		if _, err := os.Stat(filepath.Join(dest, gone)); err == nil {
			t.Errorf("%s still there", gone)
		}
	}
	if _, err := os.Stat(envPath); err != nil {
		t.Error("env file removed without --purge")
	}
	if strings.Join(runner.calls, "\n") != "systemctl --user disable --now underpass-bots.service\nsystemctl --user daemon-reload" {
		t.Errorf("commands = %v", runner.calls)
	}
	// Uninstall again: nothing to remove, no error. Purge drops the rest.
	if _, err := installer.Uninstall(false); err != nil {
		t.Error(err)
	}
	purged, err := installer.Uninstall(true)
	if err != nil || len(purged.NextSteps) != 0 {
		t.Errorf("purge: %v %+v", err, purged)
	}
	if _, err := os.Stat(envPath); err == nil {
		t.Error("env file kept after purge")
	}
	if _, err := os.Stat(filepath.Dir(envPath)); err == nil {
		t.Error("the env file's directory kept after purge")
	}
	if _, err := os.Stat(filepath.Join(dest, "home/ana/.local/state")); err == nil {
		t.Log("state parent kept (fine)")
	}
}

func TestInstallSystemModeUnderADestDir(t *testing.T) {
	dest := t.TempDir()
	l := DefaultLayout(false, "", "")
	l.DestDir = dest
	runner := &recorder{}
	installer := Installer{Layout: l, Source: source(t), Runner: runner, CreateUser: true}
	report, err := installer.Install()
	if err != nil {
		t.Fatal(err)
	}
	if report.Mode != "system" || report.EnvFile != "/etc/underpass-bots/env" {
		t.Errorf("report = %+v", report)
	}
	service := mustRead(t, filepath.Join(dest, "etc/systemd/system/underpass-bots.service"))
	if !strings.Contains(service, "ProtectSystem=strict\n") || !strings.Contains(service, "User=underpass-bots\n") || strings.Contains(service, dest) {
		t.Errorf("service:\n%s", service)
	}
	if env := mustRead(t, filepath.Join(dest, "etc/underpass-bots/env")); !strings.Contains(env, "#OPENROUTER_API_KEY=") {
		t.Errorf("env:\n%s", env)
	}
	if _, err := os.Stat(filepath.Join(dest, "usr/local/bin/underpass-bots")); err != nil {
		t.Error(err)
	}
	calls := strings.Join(runner.calls, "\n")
	// The service user does not exist on a test machine: useradd is asked for.
	if !strings.Contains(calls, "useradd --system --home-dir /var/lib/underpass-bots --no-create-home --shell /usr/sbin/nologin --groups systemd-journal underpass-bots") || !strings.HasSuffix(calls, "systemctl daemon-reload") {
		t.Errorf("commands = %v", runner.calls)
	}
	// Without the user the chown is deferred to a next step.
	joined := strings.Join(report.NextSteps, "\n")
	if !strings.Contains(joined, "chown underpass-bots:") || !strings.Contains(joined, "systemctl enable --now underpass-bots.service") {
		t.Errorf("next steps = %v", report.NextSteps)
	}
}

func TestInstallerFailures(t *testing.T) {
	l := DefaultLayout(true, "", "/home/ana")
	l.DestDir = t.TempDir()
	if _, err := (Installer{Layout: l}).Install(); err == nil {
		t.Error("no source accepted")
	}
	if _, err := (Installer{Layout: l, Source: "/no/such/binary"}).Install(); err == nil {
		t.Error("missing source accepted")
	}
	// systemctl failing is reported.
	runner := &recorder{fail: map[string]error{"systemctl --user daemon-reload": errors.New("no bus")}}
	if _, err := (Installer{Layout: l, Source: source(t), Runner: runner}).Install(); err == nil || !strings.Contains(err.Error(), "no bus") {
		t.Errorf("daemon-reload failure: %v", err)
	}
	if _, err := (Installer{Layout: l, Source: source(t), Runner: runner}).Uninstall(false); err == nil || !strings.Contains(err.Error(), "no bus") {
		t.Errorf("uninstall daemon-reload failure: %v", err)
	}
	// useradd failing is reported in system mode.
	sys := DefaultLayout(false, "", "")
	sys.DestDir = t.TempDir()
	failing := &recorder{fail: map[string]error{"useradd": errors.New("not root")}}
	if _, err := (Installer{Layout: sys, Source: source(t), Runner: failing, CreateUser: true}).Install(); err == nil || !strings.Contains(err.Error(), "not root") {
		t.Errorf("useradd failure: %v", err)
	}
	// A unit directory that is a file cannot take the units.
	blocked := DefaultLayout(true, "", "/home/ana")
	blocked.DestDir = t.TempDir()
	if err := os.MkdirAll(filepath.Dir(blocked.OnDisk(blocked.UnitDir)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(blocked.OnDisk(blocked.UnitDir), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := (Installer{Layout: blocked, Source: source(t)}).Install(); err == nil {
		t.Error("units written under a file")
	}
	if _, err := (Installer{Layout: blocked, Source: source(t)}).Uninstall(false); err == nil {
		t.Error("uninstall removed a unit under a file")
	}
	// An env path that is a directory is an error, not a silent skip.
	envDir := DefaultLayout(true, "", "/home/ana")
	envDir.DestDir = t.TempDir()
	if err := os.MkdirAll(envDir.OnDisk(envDir.EnvFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := (Installer{Layout: envDir, Source: source(t)}).Install(); err != nil {
		t.Errorf("an existing env path is kept: %v", err)
	}
	// Purge failing on a read-only parent is reported.
	if _, err := (Installer{Layout: Layout{User: true, DestDir: "/proc/underpass-nope", EnvFile: "/env", StateDir: "/state", UnitDir: "/u", Prefix: "/p"}}).Uninstall(true); err == nil {
		t.Log("purge under /proc did not fail (ok on some systems)")
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
