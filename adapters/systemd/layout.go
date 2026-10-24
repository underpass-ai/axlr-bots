// Package systemd installs the platform on a Linux host: the control
// plane and the node agent binaries, their two units and the environment
// file that holds the model key. The primary layout is user-level: no
// root, everything under ~/.local, ~/.config and systemctl --user (with
// loginctl enable-linger to keep the services up after logout). The
// system-wide layout (a dedicated user under /usr/local, /etc and
// /var/lib) is the optional variant. It also removes them.
package systemd

import "path/filepath"

// Names the installer uses.
const (
	ServiceName    = "axlr-bots.service"
	NodeUnitName   = "axlr-node.service"
	ServiceUser    = "axlr-bots"
	BinaryName     = "axlr-bots"
	NodeBinaryName = "axlr-node"
	// JournalGroup lets the system-mode service user read the journal.
	JournalGroup = "systemd-journal"
)

// Layout is where everything goes. Every path is absolute; DestDir, when
// set, is put in front of all of them on disk (packaging, tests) while
// the rendered units keep the real paths.
type Layout struct {
	// System is the system-wide variant: a dedicated user, /usr/local,
	// /etc and /var/lib. Off, the layout is the user's own.
	System bool
	// Prefix holds bin/axlr-bots and bin/axlr-node: ~/.local,
	// or /usr/local.
	Prefix string
	// StateDir is the control plane's state: ~/.local/state/axlr-bots,
	// or /var/lib/axlr-bots.
	StateDir string
	// NodeStateDir is the node's state (traces, policies, workspaces):
	// ~/.local/state/axlr-node, or /var/lib/axlr-node.
	NodeStateDir string
	// UnitDir holds the units: ~/.config/systemd/user, or /etc/systemd/system.
	UnitDir string
	// EnvFile holds the model key: ~/.config/axlr-bots/env, or
	// /etc/axlr-bots/env.
	EnvFile string
	// AXLRBin is the axlr-tui the node runs; a bare name is looked up
	// on the service's PATH.
	AXLRBin string
	// Listen is the TLS address the control plane serves nodes on
	// (127.0.0.1:7878, or 0.0.0.0:7878 for other hosts); empty serves the
	// socket only.
	Listen string
	// ServerNames are extra names the control plane's certificate is
	// valid for, comma-separated: the public name nodes dial.
	ServerNames string
	// APIListen is the TLS address the control plane serves operators
	// and webhook senders on (127.0.0.1:7879); empty serves operators on
	// the socket only.
	APIListen string
	// Control is the control plane the node unit talks to; empty means
	// the socket on this host.
	Control string
	// NodeOnly installs the node agent alone: a host that joins a
	// control plane elsewhere.
	NodeOnly bool
	// DestDir is prepended to every path on disk.
	DestDir string
}

// NodeControl is the control plane the node unit is given: Control, or
// the socket on this host.
func (l Layout) NodeControl() string {
	if l.Control != "" {
		return l.Control
	}
	return "unix://" + l.SocketPath()
}

// DefaultLayout is the layout of one mode: the user's own under home
// (prefix ~/.local), or system-wide under prefix (default /usr/local).
func DefaultLayout(system bool, prefix, home string) Layout {
	if system {
		if prefix == "" {
			prefix = "/usr/local"
		}
		return Layout{
			System: true, Prefix: prefix, StateDir: "/var/lib/axlr-bots", NodeStateDir: "/var/lib/axlr-node",
			UnitDir: "/etc/systemd/system", EnvFile: "/etc/axlr-bots/env", AXLRBin: "axlr-tui",
		}
	}
	if prefix == "" {
		prefix = filepath.Join(home, ".local")
	}
	return Layout{
		Prefix:       prefix,
		StateDir:     filepath.Join(home, ".local", "state", "axlr-bots"),
		NodeStateDir: filepath.Join(home, ".local", "state", "axlr-node"),
		UnitDir:      filepath.Join(home, ".config", "systemd", "user"),
		EnvFile:      filepath.Join(home, ".config", "axlr-bots", "env"),
		AXLRBin:      "axlr-tui",
	}
}

// BinaryPath and NodeBinaryPath are the installed binaries, as the
// units name them.
func (l Layout) BinaryPath() string     { return filepath.Join(l.Prefix, "bin", BinaryName) }
func (l Layout) NodeBinaryPath() string { return filepath.Join(l.Prefix, "bin", NodeBinaryName) }

// SocketPath is the control plane's socket, under its state directory.
func (l Layout) SocketPath() string { return filepath.Join(l.StateDir, "axlr-bots.sock") }

// ServicePath and NodeUnitPath are the unit files.
func (l Layout) ServicePath() string  { return filepath.Join(l.UnitDir, ServiceName) }
func (l Layout) NodeUnitPath() string { return filepath.Join(l.UnitDir, NodeUnitName) }

// OnDisk is the path with DestDir in front.
func (l Layout) OnDisk(path string) string {
	if l.DestDir == "" {
		return path
	}
	return filepath.Join(l.DestDir, path)
}

// Systemctl is the systemctl invocation for the mode.
func (l Layout) Systemctl() []string {
	if l.System {
		return []string{"systemctl"}
	}
	return []string{"systemctl", "--user"}
}
