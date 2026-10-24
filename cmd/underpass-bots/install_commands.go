package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/underpass-ai/underpass-bots/adapters/systemd"
)

// installFlags are the flags install and uninstall share: the mode and
// the layout.
func installFlags(name string, args []string, stderr io.Writer) (*flag.FlagSet, *systemd.Layout, *bool, error) {
	flags := flag.NewFlagSet(name, flag.ContinueOnError)
	flags.SetOutput(stderr)
	user := flags.Bool("user", false, "install under ~/.local and systemctl --user (no root needed)")
	prefix := flags.String("prefix", "", "where bin/underpass-bots goes (default /usr/local, or ~/.local with --user)")
	state := flags.String("state", "", "the state directory (default /var/lib/underpass-bots, or ~/.local/state/underpass-bots with --user)")
	unitDir := flags.String("unit-dir", "", "where the units go (default /etc/systemd/system, or ~/.config/systemd/user with --user)")
	envFile := flags.String("env-file", "", "the environment file (default /etc/underpass-bots/env, or ~/.config/underpass-bots/env with --user)")
	axlrBin := flags.String("axlr-bin", defaultAXLRBinary(), "the axlr-tui the service runs (also UNDERPASS_AXLR_BIN)")
	destDir := flags.String("destdir", "", "put every path under this directory on disk (packaging, tests)")
	noSystemctl := flags.Bool("no-systemctl", false, "write the files but run no systemctl or useradd")
	if err := flags.Parse(args); err != nil {
		return nil, nil, nil, err
	}
	if flags.NArg() != 0 {
		return nil, nil, nil, fmt.Errorf("%s takes flags only", name)
	}
	home, _ := os.UserHomeDir()
	layout := systemd.DefaultLayout(*user, *prefix, home)
	if *state != "" {
		layout.StateDir = *state
	}
	if *unitDir != "" {
		layout.UnitDir = *unitDir
	}
	if *envFile != "" {
		layout.EnvFile = *envFile
	}
	layout.AXLRBin = *axlrBin
	layout.DestDir = *destDir
	return flags, &layout, noSystemctl, nil
}

// install puts the running binary, the units and the environment file on
// the host. The model key is taken from OPENROUTER_API_KEY in the
// installer's own environment when set, and never printed.
func install(args []string, stdout, stderr io.Writer) int {
	flags, layout, noSystemctl, err := installFlags("install", args, stderr)
	if err != nil {
		fmt.Fprintln(stderr, "underpass-bots:", err)
		return exitUsage
	}
	_ = flags
	executable, err := os.Executable()
	if err != nil {
		return fail(stderr, err)
	}
	installer := systemd.Installer{Layout: *layout, Source: executable, Key: os.Getenv("OPENROUTER_API_KEY"), CreateUser: true}
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
		fmt.Fprintln(stderr, "underpass-bots:", err)
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
