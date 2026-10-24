package axlr

import (
	"context"
	"os/exec"
	"strings"
	"time"
)

// policyFlag is the runtime flag that takes the execution's policy.
const policyFlag = "-policy"

// helpTimeout bounds the one `--help` run that tells whether the binary
// takes --policy.
const helpTimeout = 15 * time.Second

// supportsPolicy reports whether the binary takes --policy, by reading
// its --help once per Executor. A binary older than the policy (the
// contract build, 0.13.0) does not; the control plane then records the
// policy without passing it and keeps the run at the approval level
// that lets nothing through without a person.
func (e *Executor) supportsPolicy(ctx context.Context) bool {
	e.detect.Do(func() {
		helpCtx, cancel := context.WithTimeout(ctx, helpTimeout)
		defer cancel()
		args := append(append([]string{}, e.Args...), "--help")
		out, _ := exec.CommandContext(helpCtx, e.binary(), args...).CombinedOutput()
		e.policySupported = strings.Contains(string(out), policyFlag)
	})
	return e.policySupported
}
