package axlr

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"syscall"
	"time"

	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

const (
	// skillsFlag is the runtime's input for an execution's catalog skills
	// (AXLR's docs/runtime-contract.md, "Skills per execution"), looked
	// for in the binary's --help as the other flags are. It is a flag's
	// line, so a mention of --skills in another flag's text is not taken
	// for it.
	skillsFlag = "-skills string"
	// toolSkill is the host tool the model opens a catalog skill with.
	toolSkill = "axlr_skill"
	// skillsTimeout bounds finding and installing an execution's skills;
	// the run's own timeout, when shorter, bounds them too.
	skillsTimeout = 3 * time.Minute
)

// SupportsSkills reports whether the binary takes --skills: what a node
// tells the control plane as a capability.
func (e *Executor) SupportsSkills(ctx context.Context) bool { return e.detectFeatures(ctx).skills }

// withSkillTool adds the tool that reads a catalog skill to an execution
// with pins: an allow list forbids every tool it does not name, and the
// model could otherwise not open the skills it was given. AXLR counts it
// as a read and serves only the pinned versions. An empty list stays
// empty: it means the level alone decides.
func withSkillTool(allowed []string, pins domain.SkillPins) []string {
	if pins.IsEmpty() || len(allowed) == 0 || slices.Contains(allowed, toolSkill) {
		return allowed
	}
	return append(slices.Clone(allowed), toolSkill)
}

// prepareSkills installs the pins this host does not have and writes the
// execution's --skills file. A binary that takes the flag always gets
// it: with none pinned the list is empty, so a bot without skills does
// not read the ones installed here for another bot. An error says why the
// execution must not start.
func (e *Executor) prepareSkills(ctx context.Context, execution domain.ExecutionID, pins domain.SkillPins, runTimeout time.Duration) ([]string, error) {
	if !e.detectFeatures(ctx).skills {
		if pins.IsEmpty() {
			return nil, nil
		}
		return nil, errors.New("this node's axlr-tui takes no pinned skills (no --skills flag): the task does not run without them")
	}
	if err := e.installMissing(ctx, pins, min(skillsTimeout, runTimeout)); err != nil {
		return nil, err
	}
	list := pins.Snapshot()
	if list == nil {
		list = []domain.SkillPinSnapshot{}
	}
	path, err := e.writeJSON("skills", execution, list)
	if err != nil {
		return nil, fmt.Errorf("the skills file could not be written: %w", err)
	}
	return []string{"--skills", path}, nil
}

// installMissing installs each pin the host lacks, one at a time.
func (e *Executor) installMissing(ctx context.Context, pins domain.SkillPins, timeout time.Duration) error {
	if pins.IsEmpty() {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	e.skillsMu.Lock()
	defer e.skillsMu.Unlock()
	for _, pin := range pins.All() {
		if skillInstalled(pin) {
			continue
		}
		if err := e.install(ctx, pin); err != nil {
			return fmt.Errorf("catalog skill %s could not be installed on this node, and the task does not run without it: %w", pin.Key(), err)
		}
	}
	return nil
}

// install runs AXLR's own install of one exact version: `axlr-tui skills
// install`, which fetches it, verifies it against the catalog's signed
// index and keeps it beside the other versions. --yes is not a decision
// taken here: the person who wrote the bot's definition pinned this
// version, and the install's card, which would show the file's text, is
// not read. No --state: --exec takes none and reads AXLR's state of the
// user it runs as, so that is where the install must land.
func (e *Executor) install(ctx context.Context, pin domain.SkillPin) error {
	if err := os.MkdirAll(e.StateDir, 0o755); err != nil {
		return err
	}
	args := append(append([]string{}, e.Args...), "skills", "install", pin.Name()+"@"+pin.Version(), "--from", pin.Catalog(), "--yes")
	cmd := exec.CommandContext(ctx, e.binary(), args...)
	cmd.Dir = e.StateDir
	cmd.Cancel = func() error { return cmd.Process.Signal(syscall.SIGTERM) }
	cmd.WaitDelay = e.waitDelay()
	tail := &tailWriter{limit: 4096}
	cmd.Stderr = tail
	err := cmd.Run()
	if err == nil {
		return nil
	}
	if ctx.Err() != nil {
		return context.Cause(ctx)
	}
	if line := tail.lastLine(); line != "" {
		return errors.New(line)
	}
	return err
}

// skillInstalled reports whether AXLR holds the version, at the path it
// documents (docs/console.md, "Catalog skills"). It is only what saves
// the network when the version is here already: the install verifies
// what it fetches and --exec refuses a pin it cannot serve, so a wrong
// answer here cannot run a task without its skill.
func skillInstalled(pin domain.SkillPin) bool {
	state := axlrState()
	if state == "" {
		return false
	}
	_, err := os.Stat(filepath.Join(state, "skills", pin.Catalog(), pin.Name(), pin.Version(), "skill.json"))
	return err == nil
}

// axlrState is AXLR's state directory for the user this process runs as,
// resolved as axlr-tui resolves it; "" when it cannot be told.
func axlrState() string {
	base := os.Getenv("XDG_STATE_HOME")
	if !filepath.IsAbs(base) {
		home := os.Getenv("HOME")
		if !filepath.IsAbs(home) {
			return ""
		}
		base = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(base, "axlr")
}

// skillsRefused is the report of an execution that did not start for
// want of its skills: the reason, and that it wrote nothing.
func skillsRefused(err error, policy preparedPolicy) application.ExecutionReport {
	return application.ExecutionReport{Outcome: domain.ExecutionFailed, FailureReason: err.Error(), Result: domain.Result{NotStarted: true}, Policy: &policy.record}
}
