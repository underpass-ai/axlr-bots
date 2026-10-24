package systemd

import (
	"strings"
)

// RenderService is the control plane's unit. Type=simple: the daemon
// runs in the foreground and logs to the journal. The model key comes
// from the environment file, never from the unit. System mode runs a
// dedicated user with the filesystem read-only but the state directory;
// user mode cannot sandbox the filesystem (ProtectSystem= and
// ProtectHome= need privileges a user manager lacks), so it leaves them
// out and says so.
func RenderService(l Layout) string {
	var b strings.Builder
	b.WriteString("[Unit]\n")
	b.WriteString("Description=Underpass Bots control plane (SRE host bot)\n")
	b.WriteString("Documentation=https://github.com/underpass-ai/underpass-bots\n")
	if l.User {
		b.WriteString("After=default.target\n")
	} else {
		b.WriteString("After=network-online.target\nWants=network-online.target\n")
	}
	b.WriteString("\n[Service]\n")
	b.WriteString("Type=simple\n")
	if !l.User {
		b.WriteString("User=" + ServiceUser + "\n")
		b.WriteString("Group=" + ServiceUser + "\n")
		b.WriteString("SupplementaryGroups=" + JournalGroup + "\n")
	}
	b.WriteString("EnvironmentFile=-" + l.EnvFile + "\n")
	if !l.User {
		// axlr-tui keeps its settings and logs under $HOME: inside the
		// state directory, the one place the service may write.
		b.WriteString("Environment=HOME=" + l.StateDir + "\n")
	}
	b.WriteString("ExecStart=" + l.BinaryPath() + " --state " + l.StateDir + " --socket " + l.SocketPath() + " serve\n")
	b.WriteString("Restart=on-failure\n")
	b.WriteString("RestartSec=5s\n")
	// A stop sends SIGTERM to the daemon and, through it, to the running
	// axlr-tui; that execution ends Failed and is reported as such.
	b.WriteString("TimeoutStopSec=30s\n")
	b.WriteString("KillMode=mixed\n")
	b.WriteString("NoNewPrivileges=yes\n")
	if l.User {
		b.WriteString("# ProtectSystem= and ProtectHome= are not available to a user service; system mode sets them.\n")
	} else {
		b.WriteString("ProtectSystem=strict\n")
		b.WriteString("ProtectHome=yes\n")
		b.WriteString("PrivateTmp=yes\n")
		b.WriteString("ProtectKernelTunables=yes\n")
		b.WriteString("ProtectControlGroups=yes\n")
		b.WriteString("RestrictSUIDSGID=yes\n")
		if l.StateDir == "/var/lib/underpass-bots" {
			b.WriteString("StateDirectory=underpass-bots\n")
			b.WriteString("StateDirectoryMode=0750\n")
		}
		b.WriteString("ReadWritePaths=" + l.StateDir + "\n")
	}
	b.WriteString("\n[Install]\n")
	if l.User {
		b.WriteString("WantedBy=default.target\n")
	} else {
		b.WriteString("WantedBy=multi-user.target\n")
	}
	return b.String()
}

// RenderAlertUnit is the OnFailure template: underpass-bots-alert@<unit>
// submits "service <unit> failed on this host" to the running control
// plane. A watched unit attaches it with a drop-in,
// OnFailure=underpass-bots-alert@%N.service, so %i is the unit's name
// without its type and the alert command adds .service back.
func RenderAlertUnit(l Layout) string {
	var b strings.Builder
	b.WriteString("[Unit]\n")
	b.WriteString("Description=Underpass Bots: tell the SRE bot that %i failed\n")
	b.WriteString("Documentation=https://github.com/underpass-ai/underpass-bots\n")
	b.WriteString("After=" + ServiceName + "\n")
	b.WriteString("\n[Service]\n")
	b.WriteString("Type=oneshot\n")
	if !l.User {
		b.WriteString("User=" + ServiceUser + "\n")
		b.WriteString("Group=" + ServiceUser + "\n")
		b.WriteString("Environment=HOME=" + l.StateDir + "\n")
	}
	b.WriteString("EnvironmentFile=-" + l.EnvFile + "\n")
	b.WriteString("ExecStart=" + l.BinaryPath() + " --state " + l.StateDir + " --socket " + l.SocketPath() + " alert --unit %i\n")
	b.WriteString("NoNewPrivileges=yes\n")
	if !l.User {
		b.WriteString("ProtectSystem=strict\n")
		b.WriteString("ProtectHome=yes\n")
		b.WriteString("ReadWritePaths=" + l.StateDir + "\n")
	}
	return b.String()
}

// RenderDropIn is the drop-in that attaches the alert to any unit.
func RenderDropIn() string {
	return "# Added by underpass-bots: when this unit fails, the SRE bot diagnoses it.\n[Unit]\nOnFailure=underpass-bots-alert@%N.service\n"
}

// RenderEnv is the environment file. The key is written only when the
// installer has one; otherwise the line stays a commented placeholder
// for the operator to fill in.
func RenderEnv(l Layout, key string) string {
	var b strings.Builder
	b.WriteString("# Environment of underpass-bots.service and underpass-bots-alert@.service.\n")
	b.WriteString("# The model key the bot's runtime (axlr-tui, OpenRouter) uses. It lives here,\n")
	b.WriteString("# readable by root only, never in a unit file.\n")
	if key != "" {
		b.WriteString("OPENROUTER_API_KEY=" + key + "\n")
	} else {
		b.WriteString("#OPENROUTER_API_KEY=sk-or-...\n")
	}
	b.WriteString("UNDERPASS_AXLR_BIN=" + l.AXLRBin + "\n")
	b.WriteString("UNDERPASS_BOT=sre-host-bot\n")
	if l.User {
		b.WriteString("# The units the alert names belong to systemd --user.\n")
		b.WriteString("UNDERPASS_SYSTEMD_USER=1\n")
	}
	return b.String()
}
