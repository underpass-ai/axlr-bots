// Package systemd installs the control plane on a Linux host: the binary,
// the service unit, the OnFailure alert unit and the environment file
// that holds the model key, in system mode (a dedicated user under
// /usr/local, /etc and /var/lib) or in user mode (~/.local, ~/.config and
// systemctl --user, so it can be tried without root). It also removes
// them.
package systemd

import "path/filepath"

// Names the installer uses.
const (
	ServiceName   = "underpass-bots.service"
	AlertUnitName = "underpass-bots-alert@.service"
	ServiceUser   = "underpass-bots"
	BinaryName    = "underpass-bots"
	// JournalGroup lets the service user read the whole journal.
	JournalGroup = "systemd-journal"
)

// Layout is where everything goes. Every path is absolute; DestDir, when
// set, is put in front of all of them on disk (packaging, tests) while
// the rendered units keep the real paths.
type Layout struct {
	// User is the systemctl --user mode.
	User bool
	// Prefix holds bin/underpass-bots: /usr/local, or ~/.local.
	Prefix string
	// StateDir is the control plane's state: /var/lib/underpass-bots, or
	// ~/.local/state/underpass-bots.
	StateDir string
	// UnitDir holds the units: /etc/systemd/system, or ~/.config/systemd/user.
	UnitDir string
	// EnvFile holds the model key: /etc/underpass-bots/env, or
	// ~/.config/underpass-bots/env.
	EnvFile string
	// AXLRBin is the axlr-tui the service runs; a bare name is looked up
	// on the service's PATH.
	AXLRBin string
	// DestDir is prepended to every path on disk.
	DestDir string
}

// DefaultLayout is the layout of one mode: system under prefix (default
// /usr/local), or user under home.
func DefaultLayout(user bool, prefix, home string) Layout {
	if user {
		if prefix == "" {
			prefix = filepath.Join(home, ".local")
		}
		return Layout{
			User:     true,
			Prefix:   prefix,
			StateDir: filepath.Join(home, ".local", "state", "underpass-bots"),
			UnitDir:  filepath.Join(home, ".config", "systemd", "user"),
			EnvFile:  filepath.Join(home, ".config", "underpass-bots", "env"),
			AXLRBin:  "axlr-tui",
		}
	}
	if prefix == "" {
		prefix = "/usr/local"
	}
	return Layout{
		Prefix:   prefix,
		StateDir: "/var/lib/underpass-bots",
		UnitDir:  "/etc/systemd/system",
		EnvFile:  "/etc/underpass-bots/env",
		AXLRBin:  "axlr-tui",
	}
}

// BinaryPath is where the installed binary lives, as the units name it.
func (l Layout) BinaryPath() string { return filepath.Join(l.Prefix, "bin", BinaryName) }

// SocketPath is the daemon's socket, under the state directory.
func (l Layout) SocketPath() string { return filepath.Join(l.StateDir, "underpass-bots.sock") }

// ServicePath and AlertUnitPath are the unit files.
func (l Layout) ServicePath() string   { return filepath.Join(l.UnitDir, ServiceName) }
func (l Layout) AlertUnitPath() string { return filepath.Join(l.UnitDir, AlertUnitName) }

// OnDisk is the path with DestDir in front.
func (l Layout) OnDisk(path string) string {
	if l.DestDir == "" {
		return path
	}
	return filepath.Join(l.DestDir, path)
}

// Systemctl is the systemctl invocation for the mode.
func (l Layout) Systemctl() []string {
	if l.User {
		return []string{"systemctl", "--user"}
	}
	return []string{"systemctl"}
}
