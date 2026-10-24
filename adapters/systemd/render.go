package systemd

import "strings"

// RenderService is the control plane's unit. Type=simple: the daemon
// runs in the foreground and logs to the journal. The model key comes
// from the environment file, never from the unit. The system-wide
// variant runs a dedicated user with the filesystem read-only but the
// state directory; a user service cannot sandbox the filesystem
// (ProtectSystem= and ProtectHome= need privileges a user manager
// lacks), so the user-level unit leaves them out and says so.
func RenderService(l Layout) string {
	var b strings.Builder
	b.WriteString("[Unit]\n")
	b.WriteString("Description=AXLR Bots control plane\n")
	b.WriteString("Documentation=https://github.com/underpass-ai/axlr-bots\n")
	if l.System {
		b.WriteString("After=network-online.target\nWants=network-online.target\n")
	}
	b.WriteString("\n[Service]\n")
	b.WriteString("Type=simple\n")
	renderUser(&b, l)
	b.WriteString("EnvironmentFile=-" + l.EnvFile + "\n")
	if l.System {
		b.WriteString("Environment=HOME=" + l.StateDir + "\n")
	}
	b.WriteString("ExecStart=" + l.BinaryPath() + " --state " + l.StateDir + " --socket " + l.SocketPath() + " serve\n")
	renderRestart(&b)
	renderSandbox(&b, l, l.StateDir, l.StateDir == "/var/lib/axlr-bots", "axlr-bots")
	b.WriteString("\n[Install]\n")
	renderWantedBy(&b, l)
	return b.String()
}

// RenderNodeUnit is the node agent's unit: it starts after the control
// plane and talks to it over the socket; the host name is its node id
// and its host= label.
func RenderNodeUnit(l Layout) string {
	var b strings.Builder
	b.WriteString("[Unit]\n")
	b.WriteString("Description=AXLR Bots node agent\n")
	b.WriteString("Documentation=https://github.com/underpass-ai/axlr-bots\n")
	b.WriteString("After=" + ServiceName + "\nWants=" + ServiceName + "\n")
	b.WriteString("\n[Service]\n")
	b.WriteString("Type=simple\n")
	renderUser(&b, l)
	b.WriteString("EnvironmentFile=-" + l.EnvFile + "\n")
	b.WriteString("Environment=AXLR_NODE_LABELS=host=%l\n")
	if l.System {
		// axlr-tui keeps its settings and logs under $HOME: inside the
		// node's state directory, the one place the node may write.
		b.WriteString("Environment=HOME=" + l.NodeStateDir + "\n")
	}
	b.WriteString("ExecStart=" + l.NodeBinaryPath() + " --control unix://" + l.SocketPath() + " --state " + l.NodeStateDir + "\n")
	renderRestart(&b)
	renderSandbox(&b, l, l.NodeStateDir, l.NodeStateDir == "/var/lib/axlr-node", "axlr-node")
	if l.System {
		// The node's executions read the socket in the control plane's state.
		b.WriteString("ReadWritePaths=" + l.StateDir + "\n")
	}
	b.WriteString("\n[Install]\n")
	renderWantedBy(&b, l)
	return b.String()
}

func renderUser(b *strings.Builder, l Layout) {
	if l.System {
		b.WriteString("User=" + ServiceUser + "\n")
		b.WriteString("Group=" + ServiceUser + "\n")
		b.WriteString("SupplementaryGroups=" + JournalGroup + "\n")
	}
}

func renderRestart(b *strings.Builder) {
	b.WriteString("Restart=on-failure\n")
	b.WriteString("RestartSec=5s\n")
	// A stop sends SIGTERM to the process and, through it, to a running
	// axlr-tui; that execution ends Failed and is reported as such.
	b.WriteString("TimeoutStopSec=30s\n")
	b.WriteString("KillMode=mixed\n")
	b.WriteString("NoNewPrivileges=yes\n")
}

func renderSandbox(b *strings.Builder, l Layout, stateDir string, defaultState bool, stateName string) {
	if !l.System {
		b.WriteString("# ProtectSystem= and ProtectHome= are not available to a user service; the --system variant sets them.\n")
		return
	}
	b.WriteString("ProtectSystem=strict\n")
	b.WriteString("ProtectHome=yes\n")
	b.WriteString("PrivateTmp=yes\n")
	b.WriteString("ProtectKernelTunables=yes\n")
	b.WriteString("ProtectControlGroups=yes\n")
	b.WriteString("RestrictSUIDSGID=yes\n")
	if defaultState {
		b.WriteString("StateDirectory=" + stateName + "\n")
		b.WriteString("StateDirectoryMode=0750\n")
	}
	b.WriteString("ReadWritePaths=" + stateDir + "\n")
}

func renderWantedBy(b *strings.Builder, l Layout) {
	if l.System {
		b.WriteString("WantedBy=multi-user.target\n")
		return
	}
	b.WriteString("WantedBy=default.target\n")
}

// RenderEnv is the environment file both units read. The key is written
// only when the installer has one; otherwise the line stays a commented
// placeholder for the operator to fill in.
func RenderEnv(l Layout, key string) string {
	var b strings.Builder
	b.WriteString("# Environment of axlr-bots.service and axlr-node.service.\n")
	b.WriteString("# The model key the bots' runtime (axlr-tui, OpenRouter) uses. It lives here,\n")
	b.WriteString("# mode 0600, never in a unit file.\n")
	if key != "" {
		b.WriteString("OPENROUTER_API_KEY=" + key + "\n")
	} else {
		b.WriteString("#OPENROUTER_API_KEY=sk-or-...\n")
	}
	b.WriteString("AXLR_TUI=" + l.AXLRBin + "\n")
	return b.String()
}
