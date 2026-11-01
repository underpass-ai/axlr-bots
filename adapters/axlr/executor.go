// Package axlr runs a task through AXLR's one-prompt command,
// `axlr-tui --exec --json`, decides a pending call with
// `axlr-tui --session <id> --decide approve|deny --json`, and reads the
// execution result each ends with. It is the RuntimeExecutor and
// DecisionExecutor adapter of the runtime contract (AXLR
// docs/runtime-contract.md); switching to the service, axlr-serve, is
// another adapter behind the same ports.
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
	"sync"
	"syscall"
	"time"

	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
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
	// Relay is the absolute path of axlr-node, whose mcp-relay AXLR
	// starts to reach the delegation tool; empty wires no tool.
	Relay string
	// MCPConfig is the mcp.json every run loads (axlr-tui --mcp-config)
	// instead of the operator's ~/.config/axlr/mcp.json; empty keeps the
	// operator's. On 1 November 2026 an SRE bot's triage failed because
	// a documentation server in the operator's file could not be
	// reached.
	MCPConfig string

	detect   sync.Once
	features features
}

// NewExecutor runs binary (axlr-tui on PATH when empty) with its traces
// and workspaces under stateDir.
func NewExecutor(binary, stateDir string) *Executor {
	if binary == "" {
		binary = defaultBinary
	}
	return &Executor{Binary: binary, StateDir: stateDir, Approve: defaultApprove, DefaultTimeout: defaultTimeout, WaitDelay: defaultWaitDelay}
}

// Execute runs the task's objective as one prompt under the bot's policy
// and reports the result.
func (e *Executor) Execute(ctx context.Context, request application.ExecutionRequest) (application.ExecutionReport, error) {
	if request.Objective == "" {
		return application.ExecutionReport{}, errors.New("axlr: the request has no objective")
	}
	policy, err := e.preparePolicy(ctx, request.ExecutionID, request.Level, request.AllowedTools, request.ToolRules)
	if err != nil {
		return application.ExecutionReport{}, err
	}
	if err := e.wireDelegation(request.ExecutionID, request.NodeTools(), request.DelegationSocket, &policy); err != nil {
		return application.ExecutionReport{}, fmt.Errorf("axlr: delegation manifest: %w", err)
	}
	args := []string{"--exec", request.PromptText(), "--json", "--approve", policy.approve}
	if request.Model != "" {
		args = append(args, "--model", request.Model)
	}
	if e.Mode != "" {
		args = append(args, "--mode", e.Mode)
	}
	args = append(args, e.mcpConfigArgs()...)
	// The correlation ids and the budget go with the run when the binary
	// takes them (docs/runtime-contract.md, the reserved fields); the
	// correlation file is written either way and the record says whether
	// the runtime carried the ids.
	correlation, err := e.prepareCorrelation(ctx, request.ExecutionID, request.TaskID, request.BotID, request.CorrelationID, request.Budgets, e.timeout(request.Timeout), &policy)
	if err != nil {
		return application.ExecutionReport{}, err
	}
	args = append(args, policy.args...)
	args = append(args, policy.plugin...)
	args = append(args, correlation.args...)
	args = append(args, e.steerArgs(ctx, request.ExecutionID)...)
	defer e.dropSteer(request.ExecutionID)
	report, err := e.run(ctx, request.ExecutionID, request.BotID, args, request.Timeout, request.OnSaid)
	report.Policy = &policy.record
	correlate(&report, correlation)
	return report, err
}

// mcpConfigArgs names the run's mcp.json when the node has its own.
func (e *Executor) mcpConfigArgs() []string {
	if e.MCPConfig == "" {
		return nil
	}
	return []string{"--mcp-config", e.MCPConfig}
}

// Decide approves or denies the pending call of an execution that ended
// in DecisionRequired, runs the session's turn to its end under the same
// policy and reports the result, as `axlr-tui --session <id> --decide
// approve|deny --policy <file>` does.
func (e *Executor) Decide(ctx context.Context, request application.DecisionRequest) (application.ExecutionReport, error) {
	if err := request.Validate(); err != nil {
		return application.ExecutionReport{}, fmt.Errorf("axlr: %w", err)
	}
	policy, err := e.preparePolicy(ctx, request.ExecutionID, request.Level, request.AllowedTools, request.ToolRules)
	if err != nil {
		return application.ExecutionReport{}, err
	}
	if err := e.wireDelegation(request.ExecutionID, request.NodeTools(), request.DelegationSocket, &policy); err != nil {
		return application.ExecutionReport{}, fmt.Errorf("axlr: delegation manifest: %w", err)
	}
	args := []string{"--session", request.SessionID, "--decide", request.Decision, "--json", "--approve", policy.approve}
	if request.CallID != "" {
		args = append(args, "--call", request.CallID)
	}
	args = append(args, e.mcpConfigArgs()...)
	correlation, err := e.prepareCorrelation(ctx, request.ExecutionID, request.TaskID, request.BotID, request.CorrelationID, domain.Budgets{}, e.timeout(request.Timeout), &policy)
	if err != nil {
		return application.ExecutionReport{}, err
	}
	args = append(args, policy.args...)
	args = append(args, policy.plugin...)
	args = append(args, correlation.args...)
	args = append(args, e.steerArgs(ctx, request.ExecutionID)...)
	defer e.dropSteer(request.ExecutionID)
	report, err := e.run(ctx, request.ExecutionID, request.BotID, args, request.Timeout, request.OnSaid)
	report.Policy = &policy.record
	correlate(&report, correlation)
	return report, err
}

// preparedPolicy is the policy of one run as the command line carries
// it: the flags, the delegation tool's, the approval level, the object
// written and the record for the execution.
type preparedPolicy struct {
	args    []string
	plugin  []string
	approve string
	object  policyObject
	record  domain.PolicyRecord
}

// preparePolicy writes the execution's policy file and decides the run's
// flags: --policy when the binary takes it, and the approval level the
// policy needs. The level is only raised to local, which lets the
// policy's allowed commands and writes run, when the runtime enforces
// the policy; without enforcement every command waits for a person.
func (e *Executor) preparePolicy(ctx context.Context, execution domain.ExecutionID, level domain.AutonomyLevel, allowed []string, rules domain.ToolRules) (preparedPolicy, error) {
	if execution == "" {
		return preparedPolicy{}, errors.New("axlr: the request needs an execution id")
	}
	parsed, err := domain.ParseAutonomyLevel(level.String())
	if err != nil {
		return preparedPolicy{}, fmt.Errorf("axlr: %w", err)
	}
	object := renderPolicy(parsed, allowed, rules)
	path, err := e.writePolicy(execution, object)
	if err != nil {
		return preparedPolicy{}, fmt.Errorf("axlr: policy file: %w", err)
	}
	enforced := e.supportsPolicy(ctx)
	prepared := preparedPolicy{
		approve: e.approve(),
		object:  object,
		record:  domain.PolicyRecord{Level: parsed.String(), RuntimeLevel: object.Level, Enforced: enforced, Path: path},
	}
	if enforced {
		prepared.args = []string{"--policy", path}
		if approveRank(approveForLevel(object.Level)) > approveRank(prepared.approve) {
			prepared.approve = approveForLevel(object.Level)
		}
	}
	return prepared, nil
}

// approveForLevel is the run's approval level the runtime level needs:
// local for the levels whose allowed commands and writes run without a
// person (the policy decides), none where every call is a person's
// decision or a read.
func approveForLevel(runtimeLevel string) string {
	switch runtimeLevel {
	case string(domain.AutonomyActWithinPolicy), string(domain.AutonomyPropose):
		return "local"
	}
	return defaultApprove
}

// approveRank orders the approval levels: none < local < all.
func approveRank(level string) int {
	switch level {
	case "local":
		return 1
	case "all":
		return 2
	}
	return 0
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
func (e *Executor) run(ctx context.Context, execution domain.ExecutionID, bot domain.BotID, args []string, requested time.Duration, hear func(application.SaidReply)) (application.ExecutionReport, error) {
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
	said := newSaidTracker(hear)
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), maxLineBytes)
	for scanner.Scan() {
		line := scanner.Bytes()
		_, _ = trace.Write(line)
		_, _ = trace.Write([]byte{'\n'})
		parser.feed(line)
		said.feed(line)
	}
	said.flush()
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
