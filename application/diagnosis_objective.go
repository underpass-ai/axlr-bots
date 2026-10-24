package application

import (
	"strings"
	"time"
)

// DiagnosisObjective is the prompt of a host diagnosis task: it names the
// failed unit, the host and the time, forbids changes and asks for the
// evidence of vision §12.3 in a fixed order, so every report reads the
// same way.
func DiagnosisObjective(unit, host string, at time.Time, userManager bool) string {
	var b strings.Builder
	b.WriteString("Service " + unit + " failed on ")
	if host != "" {
		b.WriteString("host " + host)
	} else {
		b.WriteString("this host")
	}
	b.WriteString(" at " + at.UTC().Format(time.RFC3339) + ". Diagnose it. Do not change anything: no restarts, no edits, no writes; only read-only commands.\n")
	if userManager {
		b.WriteString("The unit runs under the systemd user manager: query it with `systemctl --user status " + unit + "` and `journalctl --user -u " + unit + " --no-pager -n 200`, and the user manager's other units the same way.\n")
	} else {
		b.WriteString("Query it with `systemctl status " + unit + "` and `journalctl -u " + unit + " --no-pager -n 200`.\n")
	}
	b.WriteString(`Also look at the host: uptime, free, df -h, ss -tlnp, ps aux, the tail of the system log (journalctl -p warning -n 100 --no-pager) and dmesg --nopager if it is readable. If a command is refused by the policy, note it and go on without it.

Write the report in Markdown with exactly these sections:

1. Timeline: the alert time and the timestamps the logs show, oldest first.
2. Services involved: the failed unit and the units or hosts it depends on, as the evidence shows.
3. Commands run: every command you ran, in order, and in one line each what it showed (cite the exact log lines that matter).
4. Hypotheses, ordered by evidence: strongest first, each with the evidence for and against it.
5. Proposed next steps: what an operator should do next, without doing it yourself.

The report must be based on what you read, not on what is usual for such a failure. Changes to the host are out of scope.`)
	return b.String()
}
