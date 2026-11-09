package axlr

import (
	"bytes"
	"context"
	"os/exec"
	"strings"
	"time"
)

// Flags of the runtime contract the binary may or may not take; each is
// looked for in the binary's --help.
const (
	policyFlag      = "-policy"
	correlationFlag = "-correlation"
	budgetFlag      = "-budget"
	executionFlag   = "-execution"
)

// helpTimeout bounds the one `--help` run that tells which flags the
// binary takes.
const helpTimeout = 15 * time.Second

// features is what the binary's --help advertised.
type features struct {
	policy, correlation, budget, execution, steer, kmpStore, skills bool
}

// SupportsPolicy reports whether the binary takes --policy: what a node
// tells the control plane as a capability.
func (e *Executor) SupportsPolicy(ctx context.Context) bool { return e.supportsPolicy(ctx) }

// SupportsCorrelation reports whether the binary takes the correlation
// ids: --correlation, or the --execution bundle that carries them.
func (e *Executor) SupportsCorrelation(ctx context.Context) bool {
	f := e.detectFeatures(ctx)
	return f.correlation || f.execution
}

// Version is the binary's version line (`axlr-tui --version`), or ""
// when it has none. AXLR prints it on stderr, as the flag package does;
// stdout wins when the binary writes there.
func (e *Executor) Version(ctx context.Context) string {
	versionCtx, cancel := context.WithTimeout(ctx, helpTimeout)
	defer cancel()
	args := append(append([]string{}, e.Args...), "--version")
	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(versionCtx, e.binary(), args...)
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return ""
	}
	out := strings.TrimSpace(stdout.String())
	if out == "" {
		out = strings.TrimSpace(stderr.String())
	}
	line, _, _ := strings.Cut(out, "\n")
	return line
}

// supportsPolicy reports whether the binary takes --policy. A binary
// older than the policy (the contract build, 0.13.0) does not; the
// control plane then records the policy without passing it and keeps
// the run at the approval level that lets nothing through without a
// person.
func (e *Executor) supportsPolicy(ctx context.Context) bool { return e.detectFeatures(ctx).policy }

// detectFeatures reads the binary's --help once per Executor.
func (e *Executor) detectFeatures(ctx context.Context) features {
	e.detect.Do(func() {
		helpCtx, cancel := context.WithTimeout(ctx, helpTimeout)
		defer cancel()
		args := append(append([]string{}, e.Args...), "--help")
		out, _ := exec.CommandContext(helpCtx, e.binary(), args...).CombinedOutput()
		help := string(out)
		e.features = features{
			policy:      strings.Contains(help, policyFlag),
			correlation: strings.Contains(help, correlationFlag),
			budget:      strings.Contains(help, budgetFlag),
			execution:   strings.Contains(help, executionFlag),
			steer:       strings.Contains(help, steerFlag),
			kmpStore:    strings.Contains(help, kmpStoreFlag),
			skills:      strings.Contains(help, skillsFlag),
		}
	})
	return e.features
}
