package systemd

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/user"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Installer puts the platform on the host as the layout says.
type Installer struct {
	Layout Layout
	// Source is the control plane binary to install, usually the running one.
	Source string
	// NodeSource is the node agent binary to install.
	NodeSource string
	// Key is the model key written to a new environment file; empty
	// leaves a placeholder.
	Key string
	// Runner runs systemctl and useradd; nil skips them (DESTDIR
	// packaging, tests).
	Runner Runner
	// CreateUser adds the service user in system mode when missing.
	CreateUser bool
}

// Install copies both binaries, writes the units and the environment
// file (an existing one is kept, so a key is never overwritten), creates
// the state directories and, system-wide, the service user, and reloads
// systemd. With NodeOnly it installs the node agent, its unit and its
// state alone: the host joins the control plane the layout names.
func (i Installer) Install() (Report, error) {
	l := i.Layout
	report := Report{Mode: mode(l), Binary: l.BinaryPath(), NodeBinary: l.NodeBinaryPath(), EnvFile: l.EnvFile, StateDir: l.StateDir, NodeStateDir: l.NodeStateDir, Socket: l.SocketPath(), Control: l.NodeControl(), Listen: l.Listen}
	if l.NodeOnly {
		report.Binary, report.StateDir, report.Socket = "", "", ""
		if l.Control == "" {
			return report, errors.New("systemd: a node-only install needs the control plane's URL (--control https://host:7878)")
		}
	}
	if (i.Source == "" && !l.NodeOnly) || i.NodeSource == "" {
		return report, errors.New("systemd: the binaries to install are not named (control plane and node)")
	}
	if l.System && i.CreateUser && i.Runner != nil {
		if _, err := user.Lookup(ServiceUser); err != nil {
			args := []string{"--system", "--home-dir", l.StateDir, "--no-create-home", "--shell", "/usr/sbin/nologin", "--groups", JournalGroup, ServiceUser}
			if err := i.Runner.Run("useradd", args...); err != nil {
				return report, fmt.Errorf("systemd: create the service user: %w", err)
			}
			report.Commands = append(report.Commands, "useradd "+strings.Join(args, " "))
		}
	}
	if !l.NodeOnly {
		if err := copyFile(i.Source, l.OnDisk(l.BinaryPath()), 0o755); err != nil {
			return report, fmt.Errorf("systemd: install the binary: %w", err)
		}
	}
	if err := copyFile(i.NodeSource, l.OnDisk(l.NodeBinaryPath()), 0o755); err != nil {
		return report, fmt.Errorf("systemd: install the node binary: %w", err)
	}
	units := map[string]string{l.NodeUnitPath(): RenderNodeUnit(l)}
	if !l.NodeOnly {
		units[l.ServicePath()] = RenderService(l)
	}
	for path, content := range units {
		if err := writeFile(l.OnDisk(path), content, 0o644); err != nil {
			return report, fmt.Errorf("systemd: write %s: %w", path, err)
		}
		report.Units = append(report.Units, path)
	}
	sort.Strings(report.Units)
	if _, err := os.Stat(l.OnDisk(l.EnvFile)); errors.Is(err, os.ErrNotExist) {
		if err := writeFile(l.OnDisk(l.EnvFile), RenderEnv(l, i.Key), 0o600); err != nil {
			return report, fmt.Errorf("systemd: write %s: %w", l.EnvFile, err)
		}
	} else if err != nil {
		return report, fmt.Errorf("systemd: %w", err)
	}
	dirs := []string{l.NodeStateDir}
	if !l.NodeOnly {
		dirs = append([]string{l.StateDir}, dirs...)
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(l.OnDisk(dir), 0o750); err != nil {
			return report, fmt.Errorf("systemd: state directory: %w", err)
		}
		if l.System {
			i.chown(dir, &report)
		}
	}
	if i.Runner != nil {
		args := append(l.Systemctl()[1:], "daemon-reload")
		if err := i.Runner.Run(l.Systemctl()[0], args...); err != nil {
			return report, fmt.Errorf("systemd: %w", err)
		}
		report.Commands = append(report.Commands, strings.Join(l.Systemctl(), " ")+" daemon-reload")
	}
	ctl := strings.Join(l.Systemctl(), " ")
	report.NextSteps = append(report.NextSteps, "put the model key in "+l.EnvFile+" if the installer had none")
	switch {
	case l.NodeOnly:
		report.NextSteps = append(report.NextSteps,
			l.NodeBinaryPath()+" enroll --control "+l.Control+" --state "+l.NodeStateDir+" --token <the token axlr-bots node enroll-token printed>",
			ctl+" enable --now "+NodeUnitName,
		)
	default:
		if l.Listen != "" {
			report.NextSteps = append(report.NextSteps, l.BinaryPath()+" --state "+l.StateDir+" ca init  # once per fleet, before the service starts")
		}
		report.NextSteps = append(report.NextSteps,
			ctl+" enable --now "+ServiceName+" "+NodeUnitName,
			l.BinaryPath()+" --state "+l.StateDir+" node list",
			l.BinaryPath()+" --state "+l.StateDir+" bot register --file examples/bot.json",
			l.BinaryPath()+" --state "+l.StateDir+" bot enable bot",
		)
	}
	if !l.System {
		report.NextSteps = append(report.NextSteps, "loginctl enable-linger "+currentUser()+"  # keep the services running after logout")
	}
	return report, nil
}

// Uninstall stops and disables the units, removes them and the
// binaries, and reloads systemd. The state directories and the
// environment file (the key) stay unless purge is set.
func (i Installer) Uninstall(purge bool) (Report, error) {
	l := i.Layout
	report := Report{Mode: mode(l), Binary: l.BinaryPath(), NodeBinary: l.NodeBinaryPath(), EnvFile: l.EnvFile, StateDir: l.StateDir, NodeStateDir: l.NodeStateDir, Socket: l.SocketPath()}
	if i.Runner != nil {
		for _, unit := range []string{NodeUnitName, ServiceName} {
			args := append(l.Systemctl()[1:], "disable", "--now", unit)
			// A unit that was never enabled is not an error worth stopping for.
			if err := i.Runner.Run(l.Systemctl()[0], args...); err == nil {
				report.Commands = append(report.Commands, strings.Join(l.Systemctl(), " ")+" disable --now "+unit)
			}
		}
	}
	for _, path := range []string{l.ServicePath(), l.NodeUnitPath(), l.BinaryPath(), l.NodeBinaryPath()} {
		if err := os.Remove(l.OnDisk(path)); err == nil {
			report.Units = append(report.Units, path)
		} else if !errors.Is(err, os.ErrNotExist) {
			return report, fmt.Errorf("systemd: remove %s: %w", path, err)
		}
	}
	if purge {
		for _, path := range []string{l.EnvFile, l.StateDir, l.NodeStateDir} {
			if err := os.RemoveAll(l.OnDisk(path)); err != nil {
				return report, fmt.Errorf("systemd: purge %s: %w", path, err)
			}
			report.Units = append(report.Units, path)
		}
		// The env file's own directory (/etc/axlr-bots) goes with it
		// when nothing else is left in it.
		if dir := filepath.Dir(l.EnvFile); filepath.Base(dir) == BinaryName {
			_ = os.Remove(l.OnDisk(dir))
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
		report.NextSteps = []string{"the state directories " + l.StateDir + " and " + l.NodeStateDir + " and " + l.EnvFile + " were kept; remove them with --purge"}
	}
	return report, nil
}

// chown gives a state directory to the service user when the user
// exists and the installer may change owners; otherwise it says so in
// the report and goes on.
func (i Installer) chown(dir string, report *Report) {
	u, err := user.Lookup(ServiceUser)
	if err != nil {
		report.NextSteps = append(report.NextSteps, "chown "+ServiceUser+": "+dir+" once the user exists")
		return
	}
	uid, _ := strconv.Atoi(u.Uid)
	gid, _ := strconv.Atoi(u.Gid)
	if err := os.Chown(i.Layout.OnDisk(dir), uid, gid); err != nil {
		report.NextSteps = append(report.NextSteps, "chown "+ServiceUser+": "+dir+" ("+err.Error()+")")
	}
}

func mode(l Layout) string {
	if l.System {
		return "system"
	}
	return "user"
}

func currentUser() string {
	if u, err := user.Current(); err == nil && u.Username != "" {
		return u.Username
	}
	return "$USER"
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
