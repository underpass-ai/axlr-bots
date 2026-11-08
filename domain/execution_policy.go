package domain

import "slices"

// ExecutionPolicy is the level, the tool allow-list and its rules one task's
// executions run under: the bot's, or what the incident trigger the task
// came through narrows them to for its phase (decided 8 November 2026).
// Under observe and propose AXLR treats the allow-listed MCP tools as
// reads, so a propose phase whose list held an apply tool would apply
// without asking: each phase names only the tools it needs.
type ExecutionPolicy struct {
	level    AutonomyLevel
	tools    AllowedTools
	rules    ToolRules
	narrowed bool
	grant    *LadderGrant
}

// Level is the autonomy level the executions run at.
func (p ExecutionPolicy) Level() AutonomyLevel { return p.level }

// Tools is the allow-list the executions run with.
func (p ExecutionPolicy) Tools() AllowedTools { return p.tools }

// Rules is the bot's tool rules within the allow-list: the allow rules
// of the tools a narrower list leaves out are dropped, every deny rule is
// kept.
func (p ExecutionPolicy) Rules() ToolRules { return p.rules }

// Unattended reports a run that acts alone: a ladder granted it the top
// step, so its one bound write takes no card. Below that level it is
// false even when the task was pinned with a grant.
func (p ExecutionPolicy) Unattended() bool {
	return p.grant != nil && p.grant.Alone && p.level == AutonomyActWithinPolicy
}

// LadderUnit is the unit pinned on the policy, and false when none was.
func (p ExecutionPolicy) LadderUnit() (LadderGrant, bool) {
	if p.grant == nil {
		return LadderGrant{}, false
	}
	return *p.grant, true
}

// Tracked keeps the unit on a policy whose apply keeps its card: the
// signals of the task fall on it.
func (p ExecutionPolicy) Tracked(unit LadderGrant) ExecutionPolicy {
	copied := unit
	copied.Alone, copied.MaxToolCalls = false, 0
	p.grant = &copied
	return p
}

// Granted is the policy of an apply the ladder lets act alone: the top
// step, the runtime's reads the phase lists plus the one write tool, and
// that tool's calls held to the incident's namespace. Nothing else of
// the phase's list stays: at act-within-policy the allow list is all
// that bounds it.
func (p ExecutionPolicy) Granted(grant LadderGrant) (ExecutionPolicy, error) {
	bound, err := grant.boundRule()
	if err != nil {
		return ExecutionPolicy{}, err
	}
	kept := []string{}
	for _, name := range p.tools.names {
		if name == grant.Tool || slices.Contains(runtimeReadTools, name) {
			kept = append(kept, name.String())
		}
	}
	tools, err := NewAllowedTools(kept)
	if err != nil {
		return ExecutionPolicy{}, err
	}
	copied := grant
	copied.Alone, copied.MaxToolCalls = true, LadderMaxToolCalls
	return ExecutionPolicy{level: AutonomyActWithinPolicy, tools: tools, rules: p.rules.Within(tools).bound(bound), narrowed: true, grant: &copied}, nil
}

// Narrowed reports a policy an incident trigger narrowed from the bot's.
func (p ExecutionPolicy) Narrowed() bool { return p.narrowed }

// PolicyFor is the policy of a task that came through the trigger: the
// level and tools of the incident trigger of that name when it names
// them, the bot's otherwise. A trigger the bot no longer has, or one of
// another type, keeps the bot's.
func (b Bot) PolicyFor(ref TriggerRef) ExecutionPolicy {
	policy := ExecutionPolicy{level: b.level, tools: b.allowedTools, rules: b.toolRules}
	if ref.Type != TriggerIncident {
		return policy
	}
	trigger, ok := b.triggers.Named(ref.Name)
	if !ok || trigger.Type() != TriggerIncident {
		return policy
	}
	if level, ok := trigger.Level(); ok && level.AtMost(b.level) {
		policy.level, policy.narrowed = level, level != b.level
	}
	if tools, ok := trigger.Tools(); ok {
		policy.tools, policy.rules, policy.narrowed = tools, b.toolRules.Within(tools), true
	}
	return policy
}

// hostWriteTools are the runtime's own tools that write or run on the
// host: a phase below act-with-approval may not list them, since no card
// would stop them (observe with local_exec even runs at the runtime's
// act-within-policy).
var hostWriteTools = []ToolName{"local_write", "local_edit", "local_exec", "axlr_deliver"}

// PolicyOf is the policy a task's executions run under now: the one
// pinned on the task at submission when its incident phase narrowed the
// bot's (domain.Task.Pin), held to what the bot allows now (never wider:
// a lower bot level or a narrower allow-list wins), or PolicyFor its
// trigger. For an incident's task, below act-with-approval the host write
// tools the bot's allow rules do not narrow are dropped, whether the level
// is the phase's, pinned, or the bot's own: no card would stop them (the
// read-only host commands of an observe bot, local_exec narrowed by
// argument rules, are kept). gone reports a pinned policy whose incident
// trigger the bot no longer has: a decision under it is refused (fail
// closed). An incident task left with no tool at all is refused (err):
// the runtime reads an empty allow-list as "the level alone decides",
// wider than any list.
func (b Bot) PolicyOf(task Task) (policy ExecutionPolicy, gone bool, err error) {
	pinned, ok := task.Pinned()
	if !ok {
		policy = b.PolicyFor(task.Trigger())
	} else {
		trigger, found := b.triggers.Named(task.Trigger().Name)
		gone = !found || trigger.Type() != TriggerIncident
		level := pinned.level
		if !level.AtMost(b.level) {
			level = b.level
		}
		var kept []ToolName
		for _, name := range pinned.tools.names {
			if b.allowedTools.Allows(name) {
				kept = append(kept, name)
			}
		}
		policy = ExecutionPolicy{level: level, tools: AllowedTools{names: kept}, narrowed: true, grant: pinned.grant}
	}
	if task.IncidentID() == "" {
		return policy, gone, nil
	}
	if !policy.level.Writes() {
		var kept []ToolName
		for _, name := range policy.tools.names {
			if slices.Contains(hostWriteTools, name) && !b.toolRules.Constrains(name) {
				policy.narrowed = true
				continue
			}
			kept = append(kept, name)
		}
		policy.tools = AllowedTools{names: kept}
	}
	policy.rules = b.toolRules.Within(policy.tools)
	if policy.grant != nil && policy.grant.Alone {
		// Held to the namespace of its grant at every level: lowering the
		// bot takes away the running alone, never the bound.
		if bound, err := policy.grant.boundRule(); err == nil {
			policy.rules = policy.rules.bound(bound)
		}
	}
	if policy.tools.Len() == 0 && (ok || b.allowedTools.Len() > 0) {
		return ExecutionPolicy{}, gone, StateError{Subject: "task " + task.ID().String(), Current: "left with no tool its bot " + b.id.String() + " still allows at level " + policy.level.String(), Attempted: "run"}
	}
	return policy, gone, nil
}

// CheckTriggers refuses an incident trigger that would widen the bot's
// policy, or remove a card the bot's level would show: a level above the
// bot's; a tool its allowedTools leave out; a level below
// act-with-approval without its own explicit tools, or with a host write
// tool among them (the operator vouches that the MCP tools such a phase
// lists are reads, as AXLR itself does under observe and propose); and a
// level or tools on a bot whose runtime is a Jev program, which does not
// take a narrowed policy. A trigger that inherits a bot level below
// act-with-approval is refused when the bot holds a host write tool its
// allow rules do not narrow (an observe bot's local_exec narrowed to
// read-only commands is the designed exception).
func (b Bot) CheckTriggers() error {
	for _, trigger := range b.triggers.List() {
		if err := b.checkLadder(trigger); err != nil {
			return err
		}
		level, leveled := trigger.Level()
		tools, listed := trigger.Tools()
		if (leveled || listed) && b.runtime.Kind() == RuntimeJev {
			return invalid("trigger "+trigger.Name(), "a Jev program takes no level or tools of a phase")
		}
		if leveled && !level.AtMost(b.level) {
			return invalid("trigger "+trigger.Name(), "level "+level.String()+" is above the bot's "+b.level.String())
		}
		effective := b.level
		if leveled {
			effective = level
		}
		if trigger.Type() == TriggerIncident && !effective.Writes() {
			switch {
			case leveled && !listed:
				return invalid("trigger "+trigger.Name(), "a level below act-with-approval needs its own tools: the bot's could write without a card")
			case listed:
				for _, write := range hostWriteTools {
					if tools.Allows(write) {
						return invalid("trigger "+trigger.Name(), "level "+effective.String()+" may not list "+write.String()+": it would run without a card")
					}
				}
			default:
				// The phase inherits the bot's level and tools.
				for _, write := range hostWriteTools {
					if b.allowedTools.Allows(write) && !b.toolRules.Constrains(write) {
						return invalid("trigger "+trigger.Name(), "the bot's level "+b.level.String()+" with "+write.String()+" and no allow rule narrowing it would run without a card")
					}
				}
			}
		}
		if !listed {
			continue
		}
		for _, name := range tools.names {
			if !b.allowedTools.Allows(name) {
				return invalid("trigger "+trigger.Name(), "tool "+name.String()+" is not one of the bot's allowedTools")
			}
		}
	}
	return nil
}
