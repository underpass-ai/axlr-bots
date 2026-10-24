package systemd

import (
	"fmt"
	"os/exec"
	"strings"
)

// Runner runs the host's commands (systemctl, useradd); tests record them.
type Runner interface {
	Run(name string, args ...string) error
}

// ExecRunner runs the commands for real.
type ExecRunner struct{}

// Run runs the command and returns its combined output in the error.
func (ExecRunner) Run(name string, args ...string) error {
	out, err := exec.Command(name, args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}
