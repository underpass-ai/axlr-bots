// Package axlr runs a task through AXLR's one-prompt command,
// `axlr-tui --exec --json`, and reads the execution result it ends with.
// It is the RuntimeExecutor adapter of the runtime contract
// (AXLR docs/runtime-contract.md); switching to the service, axlr-serve,
// is another adapter behind the same port.
package axlr

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	"github.com/underpass-ai/underpass-bots/application"
	"github.com/underpass-ai/underpass-bots/domain"
)

// Exit codes of axlr-tui --exec, as the runtime contract fixes them.
const (
	exitCompleted     = 0
	exitFailed        = 1
	exitNeedsDecision = 3
)

const (
	defaultBinary    = "axlr-tui"
	defaultApprove   = "none"
	defaultTimeout   = 15 * time.Minute
	defaultWaitDelay = 5 * time.Second
	maxLineBytes     = 8 * 1024 * 1024
)

// Executor runs axlr-tui as a subprocess, one process per execution.
type Executor struct {
	// Binary is the axlr-tui to run; a bare name is looked up on PATH.
	Binary string
	// Args go before the contract's own flags (tests use them to reach a
	// helper process).
	Args []string
	// StateDir holds traces/<execution>.jsonl and workspaces/<bot>/.
	StateDir string
	// Approve is the run's approval level: none (default), local or all.
	Approve string
	// Mode is the work mode (normal, review, writer, research); empty
	// leaves the runtime's default.
	Mode string
	// DefaultTimeout bounds a request without a timeout.
	DefaultTimeout time.Duration
	// WaitDelay is how long after SIGTERM the process is killed.
	WaitDelay time.Duration
}

// NewExecutor runs binary (axlr-tui on PATH when empty) with its traces
// and workspaces under stateDir.
func NewExecutor(binary, stateDir string) *Executor {
	if binary == "" {
		binary = defaultBinary
	}
	return &Executor{Binary: binary, StateDir: stateDir, Approve: defaultApprove, DefaultTimeout: defaultTimeout, WaitDelay: defaultWaitDelay}
}

// Execute runs the task's objective as one prompt and reports the result.
func (e *Executor) Execute(ctx context.Context, request application.ExecutionRequest) (application.ExecutionReport, error) {
	if request.Objective == "" {
		return application.ExecutionReport{}, errors.New("axlr: the request has no objective")
	}
	args := []string{"--exec", request.Objective.String(), "--json", "--approve", e.approve()}
	if request.Model != "" {
		args = append(args, "--model", request.Model)
	}
	if e.Mode != "" {
		args = append(args, "--mode", e.Mode)
	}
	// TODO(runtime-contract): pass request.AllowedTools as the per-execution
	// policy ({"policy": {"level": "observe", "tools": {"allow": [...]}}}),
	// request.Budgets as the budget and the ids as the correlation once
	// axlr-tui takes them (reserved fields of docs/runtime-contract.md,
	// Phases 1 and 2 of the master plan). Until then the allow-list is
	// recorded in the request and the execution, not enforced by AXLR.
	return e.run(ctx, request.ExecutionID, request.BotID, args, request.Timeout)
}

// Decide approves or denies the pending call of an execution that ended
// in DecisionRequired, runs the session's turn to its end and reports the
// result, as `axlr-tui --session <id> --decide approve|deny` does.
func (e *Executor) Decide(ctx context.Context, request DecisionRequest) (application.ExecutionReport, error) {
	if err := request.Validate(); err != nil {
		return application.ExecutionReport{}, err
	}
	args := []string{"--session", request.SessionID, "--decide", request.Decision, "--json", "--approve", e.approve()}
	if request.CallID != "" {
		args = append(args, "--call", request.CallID)
	}
	return e.run(ctx, request.ExecutionID, request.BotID, args, request.Timeout)
}

func (e *Executor) approve() string {
	if e.Approve == "" {
		return defaultApprove
	}
	return e.Approve
}

func (e *Executor) binary() string {
	if e.Binary == "" {
		return defaultBinary
	}
	return e.Binary
}

func (e *Executor) timeout(requested time.Duration) time.Duration {
	switch {
	case requested > 0:
		return requested
	case e.DefaultTimeout > 0:
		return e.DefaultTimeout
	}
	return defaultTimeout
}

func (e *Executor) waitDelay() time.Duration {
	if e.WaitDelay > 0 {
		return e.WaitDelay
	}
	return defaultWaitDelay
}

// run starts the process, keeps every stdout line in the trace file and
// stderr beside it, and turns what it saw into a report.
func (e *Executor) run(ctx context.Context, execution domain.ExecutionID, bot domain.BotID, args []string, requested time.Duration) (application.ExecutionReport, error) {
	if execution == "" || bot == "" {
		return application.ExecutionReport{}, errors.New("axlr: the request needs an execution id and a bot id")
	}
	workspace := filepath.Join(e.StateDir, "workspaces", bot.String())
	traceDir := filepath.Join(e.StateDir, "traces")
	for _, dir := range []string{workspace, traceDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return application.ExecutionReport{}, fmt.Errorf("axlr: %w", err)
		}
	}
	tracePath := e.tracePath(execution)
	trace, err := os.Create(tracePath)
	if err != nil {
		return application.ExecutionReport{}, fmt.Errorf("axlr: %w", err)
	}
	defer trace.Close()
	stderrFile, err := os.Create(filepath.Join(traceDir, execution.String()+".stderr.log"))
	if err != nil {
		return application.ExecutionReport{}, fmt.Errorf("axlr: %w", err)
	}
	defer stderrFile.Close()

	timeout := e.timeout(requested)
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	full := append(append([]string{}, e.Args...), args...)
	full = append(full, "--root", workspace)
	cmd := exec.CommandContext(runCtx, e.binary(), full...)
	cmd.Dir = workspace
	cmd.Cancel = func() error { return cmd.Process.Signal(syscall.SIGTERM) }
	cmd.WaitDelay = e.waitDelay()
	tail := &tailWriter{limit: 4096}
	cmd.Stderr = io.MultiWriter(stderrFile, tail)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return application.ExecutionReport{}, fmt.Errorf("axlr: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return application.ExecutionReport{}, fmt.Errorf("axlr: start %s: %w", e.binary(), err)
	}

	parser := newTurnParser()
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), maxLineBytes)
	for scanner.Scan() {
		line := scanner.Bytes()
		_, _ = trace.Write(line)
		_, _ = trace.Write([]byte{'\n'})
		parser.feed(line)
	}
	scanErr := scanner.Err()
	waitErr := cmd.Wait()
	_ = trace.Sync()

	exitCode := exitCompleted
	var exitErr *exec.ExitError
	switch {
	case waitErr == nil:
	case errors.As(waitErr, &exitErr):
		exitCode = exitErr.ExitCode()
	case runCtx.Err() != nil:
		exitCode = -1
	default:
		return application.ExecutionReport{TracePath: tracePath}, fmt.Errorf("axlr: wait for %s: %w", e.binary(), waitErr)
	}

	var report application.ExecutionReport
	switch {
	case errors.Is(runCtx.Err(), context.DeadlineExceeded):
		report = parser.interrupted(fmt.Sprintf("timed out after %s", timeout))
	case runCtx.Err() != nil:
		report = parser.interrupted("cancelled: " + runCtx.Err().Error())
	case scanErr != nil:
		report = parser.interrupted("reading runtime output: " + scanErr.Error())
	default:
		report = parser.report(exitCode, tail.lastLine())
	}
	report.TracePath = tracePath
	return report, nil
}
