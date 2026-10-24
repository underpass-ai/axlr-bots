package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/underpass-ai/axlr-bots/adapters/systemd"
)

// installFlags are the flags install and uninstall share: the mode and
// the layout.
func installFlags(name string, args []string, stderr io.Writer) (*flag.FlagSet, *systemd.Layout, *bool, error) {
	flags := flag.NewFlagSet(name, flag.ContinueOnError)
	flags.SetOutput(stderr)
	system := flags.Bool("system", false, "the system-wide variant: a dedicated user, /usr/local, /etc and /var/lib (needs root); default: the user's own under ~/.local and systemctl --user")
	prefix := flags.String("prefix", "", "where bin/axlr-bots and bin/axlr-node go (default ~/.local, or /usr/local with --system)")
	state := flags.String("state", "", "the control plane's state directory (default ~/.local/state/axlr-bots, or /var/lib/axlr-bots with --system)")
	nodeState := flags.String("node-state", "", "the node's state directory (default ~/.local/state/axlr-node, or /var/lib/axlr-node with --system)")
	unitDir := flags.String("unit-dir", "", "where the units go (default ~/.config/systemd/user, or /etc/systemd/system with --system)")
	envFile := flags.String("env-file", "", "the environment file (default ~/.config/axlr-bots/env, or /etc/axlr-bots/env with --system)")
	axlrBin := flags.String("axlr-bin", defaultAXLRBinary(), "the axlr-tui the node runs (also AXLR_TUI)")
	nodeBinary := flags.String("node-binary", "", "the axlr-node binary to install (default: next to this binary)")
	listen := flags.String("listen", "", "the TLS address the control plane serves nodes on, such as 127.0.0.1:7878 or 0.0.0.0:7878 (default: the socket only)")
	serverNames := flags.String("server-names", "", "names and addresses nodes dial the control plane by, comma-separated, for its certificate")
	control := flags.String("control", "", "the control plane the node agent talks to, https://host:7878 (default: the socket on this host)")
	nodeOnly := flags.Bool("node-only", false, "install the node agent alone, for a host that joins a control plane elsewhere (needs --control)")
	destDir := flags.String("destdir", "", "put every path under this directory on disk (packaging, tests)")
	noSystemctl := flags.Bool("no-systemctl", false, "write the files but run no systemctl or useradd")
	if err := flags.Parse(args); err != nil {
		return nil, nil, nil, err
	}
	if flags.NArg() != 0 {
		return nil, nil, nil, fmt.Errorf("%s takes flags only", name)
	}
	home, _ := os.UserHomeDir()
	layout := systemd.DefaultLayout(*system, *prefix, home)
	if *state != "" {
		layout.StateDir = *state
	}
	if *nodeState != "" {
		layout.NodeStateDir = *nodeState
	}
	if *unitDir != "" {
		layout.UnitDir = *unitDir
	}
	if *envFile != "" {
		layout.EnvFile = *envFile
	}
	layout.AXLRBin = *axlrBin
	layout.Listen = *listen
	layout.ServerNames = *serverNames
	layout.Control = *control
	layout.NodeOnly = *nodeOnly
	layout.DestDir = *destDir
	nodeSource = *nodeBinary
	return flags, &layout, noSystemctl, nil
}

// nodeSource is the --node-binary of the last parse; empty means next
// to the running binary.
var nodeSource string

// install puts the running binary, the node binary found beside it, the
// two units and the environment file on the host. The model key is
// taken from OPENROUTER_API_KEY in the installer's own environment when
// set, and never printed.
func install(args []string, stdout, stderr io.Writer) int {
	_, layout, noSystemctl, err := installFlags("install", args, stderr)
	if err != nil {
		fmt.Fprintln(stderr, "axlr-bots:", err)
		return exitUsage
	}
	executable, err := os.Executable()
	if err != nil {
		return fail(stderr, err)
	}
	node := nodeSource
	if node == "" {
		node = filepath.Join(filepath.Dir(executable), systemd.NodeBinaryName)
	}
	installer := systemd.Installer{Layout: *layout, Source: executable, NodeSource: node, Key: os.Getenv("OPENROUTER_API_KEY"), CreateUser: true}
	if !*noSystemctl {
		installer.Runner = systemd.ExecRunner{}
	}
	report, err := installer.Install()
	if err != nil {
		return fail(stderr, err)
	}
	printJSON(stdout, report)
	return exitOK
}

// uninstall reverses install; the state and the environment file stay
// unless --purge is given.
func uninstall(args []string, stdout, stderr io.Writer) int {
	purge := false
	filtered := make([]string, 0, len(args))
	for _, a := range args {
		if a == "--purge" || a == "-purge" {
			purge = true
			continue
		}
		filtered = append(filtered, a)
	}
	_, layout, noSystemctl, err := installFlags("uninstall", filtered, stderr)
	if err != nil {
		fmt.Fprintln(stderr, "axlr-bots:", err)
		return exitUsage
	}
	installer := systemd.Installer{Layout: *layout}
	if !*noSystemctl {
		installer.Runner = systemd.ExecRunner{}
	}
	report, err := installer.Uninstall(purge)
	if err != nil {
		return fail(stderr, err)
	}
	printJSON(stdout, report)
	return exitOK
}
