package systemd

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
)

// Installer puts the control plane on the host as the layout says.
type Installer struct {
	Layout Layout
	// Source is the binary to install, usually the running one.
	Source string
	// Key is the model key written to a new environment file; empty
	// leaves a placeholder.
	Key string
	// Runner runs systemctl and useradd; nil skips them (DESTDIR
	// packaging, tests).
	Runner Runner
	// CreateUser adds the service user in system mode when missing.
	CreateUser bool
}

// Install copies the binary, writes the units and the environment file
// (an existing one is kept, so a key is never overwritten), creates the
// state directory and the service user, and reloads systemd.
func (i Installer) Install() (Report, error) {
	l := i.Layout
	report := Report{Mode: mode(l), Binary: l.BinaryPath(), EnvFile: l.EnvFile, StateDir: l.StateDir, Socket: l.SocketPath()}
	if i.Source == "" {
		return report, errors.New("systemd: the binary to install is not named")
	}
	if !l.User && i.CreateUser && i.Runner != nil {
		if _, err := user.Lookup(ServiceUser); err != nil {
			args := []string{"--system", "--home-dir", l.StateDir, "--no-create-home", "--shell", "/usr/sbin/nologin", "--groups", JournalGroup, ServiceUser}
			if err := i.Runner.Run("useradd", args...); err != nil {
				return report, fmt.Errorf("systemd: create the service user: %w", err)
			}
			report.Commands = append(report.Commands, "useradd "+strings.Join(args, " "))
		}
	}
	if err := copyFile(i.Source, l.OnDisk(l.BinaryPath()), 0o755); err != nil {
		return report, fmt.Errorf("systemd: install the binary: %w", err)
	}
	for path, content := range map[string]string{l.ServicePath(): RenderService(l), l.AlertUnitPath(): RenderAlertUnit(l)} {
		if err := writeFile(l.OnDisk(path), content, 0o644); err != nil {
			return report, fmt.Errorf("systemd: write %s: %w", path, err)
		}
		report.Units = append(report.Units, path)
	}
	if _, err := os.Stat(l.OnDisk(l.EnvFile)); errors.Is(err, os.ErrNotExist) {
		if err := writeFile(l.OnDisk(l.EnvFile), RenderEnv(l, i.Key), 0o600); err != nil {
			return report, fmt.Errorf("systemd: write %s: %w", l.EnvFile, err)
		}
	} else if err != nil {
		return report, fmt.Errorf("systemd: %w", err)
	}
	if err := os.MkdirAll(l.OnDisk(l.StateDir), 0o750); err != nil {
		return report, fmt.Errorf("systemd: state directory: %w", err)
	}
	if !l.User {
		i.chownState(&report)
	}
	if i.Runner != nil {
		args := append(l.Systemctl()[1:], "daemon-reload")
		if err := i.Runner.Run(l.Systemctl()[0], args...); err != nil {
			return report, fmt.Errorf("systemd: %w", err)
		}
		report.Commands = append(report.Commands, strings.Join(l.Systemctl(), " ")+" daemon-reload")
	}
	ctl := strings.Join(l.Systemctl(), " ")
	report.NextSteps = append(report.NextSteps,
		"put the model key in "+l.EnvFile+" if the installer had none",
		ctl+" enable --now "+ServiceName,
		l.BinaryPath()+" --state "+l.StateDir+" bot register --file examples/sre-host-bot.json",
		l.BinaryPath()+" --state "+l.StateDir+" bot enable sre-host-bot",
		"attach the alert to a unit: "+ctl+" edit <unit> and add "+strings.TrimSpace(strings.ReplaceAll(RenderDropIn(), "\n", " ")),
	)
	return report, nil
}

// Uninstall stops and disables the units, removes them and the binary,
// and reloads systemd. The state directory and the environment file
// (the key) stay unless purge is set.
func (i Installer) Uninstall(purge bool) (Report, error) {
	l := i.Layout
	report := Report{Mode: mode(l), Binary: l.BinaryPath(), EnvFile: l.EnvFile, StateDir: l.StateDir, Socket: l.SocketPath()}
	if i.Runner != nil {
		for _, unit := range []string{ServiceName} {
			args := append(l.Systemctl()[1:], "disable", "--now", unit)
			// A unit that was never enabled is not an error worth stopping for.
			if err := i.Runner.Run(l.Systemctl()[0], args...); err == nil {
				report.Commands = append(report.Commands, strings.Join(l.Systemctl(), " ")+" disable --now "+unit)
			}
		}
	}
	for _, path := range []string{l.ServicePath(), l.AlertUnitPath(), l.BinaryPath()} {
		if err := os.Remove(l.OnDisk(path)); err == nil {
			report.Units = append(report.Units, path)
		} else if !errors.Is(err, os.ErrNotExist) {
			return report, fmt.Errorf("systemd: remove %s: %w", path, err)
		}
	}
	if purge {
		for _, path := range []string{l.EnvFile, l.StateDir} {
			if err := os.RemoveAll(l.OnDisk(path)); err != nil {
				return report, fmt.Errorf("systemd: purge %s: %w", path, err)
			}
			report.Units = append(report.Units, path)
		}
	}
	if i.Runner != nil {
		args := append(l.Systemctl()[1:], "daemon-reload")
		if err := i.Runner.Run(l.Systemctl()[0], args...); err != nil {
			return report, fmt.Errorf("systemd: %w", err)
		}
		report.Commands = append(report.Commands, strings.Join(l.Systemctl(), " ")+" daemon-reload")
	}
	if !purge {
		report.NextSteps = []string{"the state directory " + l.StateDir + " and " + l.EnvFile + " were kept; remove them with --purge"}
	}
	return report, nil
}

// chownState gives the state directory to the service user when the
// user exists and the installer may change owners; otherwise it says so
// in the report and goes on.
func (i Installer) chownState(report *Report) {
	u, err := user.Lookup(ServiceUser)
	if err != nil {
		report.NextSteps = append(report.NextSteps, "chown "+ServiceUser+": "+i.Layout.StateDir+" once the user exists")
		return
	}
	uid, _ := strconv.Atoi(u.Uid)
	gid, _ := strconv.Atoi(u.Gid)
	if err := os.Chown(i.Layout.OnDisk(i.Layout.StateDir), uid, gid); err != nil {
		report.NextSteps = append(report.NextSteps, "chown "+ServiceUser+": "+i.Layout.StateDir+" ("+err.Error()+")")
	}
}

func mode(l Layout) string {
	if l.User {
		return "user"
	}
	return "system"
}

func writeFile(path, content string, perm os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), perm)
}

func copyFile(source, target string, perm os.FileMode) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	tmp := target + ".tmp"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	// Rename over a binary that may be running: the old inode lives on
	// for the running service until it restarts.
	return os.Rename(tmp, target)
}
