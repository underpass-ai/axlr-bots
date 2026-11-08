package domain

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
}

// Level is the autonomy level the executions run at.
func (p ExecutionPolicy) Level() AutonomyLevel { return p.level }

// Tools is the allow-list the executions run with.
func (p ExecutionPolicy) Tools() AllowedTools { return p.tools }

// Rules is the bot's tool rules within the allow-list: the allow rules
// of the tools a narrower list leaves out are dropped, every deny rule is
// kept.
func (p ExecutionPolicy) Rules() ToolRules { return p.rules }

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

// CheckTriggers refuses an incident trigger that would widen the bot's
// policy: a level above the bot's, or a tool its allowedTools leave out.
func (b Bot) CheckTriggers() error {
	for _, trigger := range b.triggers.List() {
		if level, ok := trigger.Level(); ok && !level.AtMost(b.level) {
			return invalid("trigger "+trigger.Name(), "level "+level.String()+" is above the bot's "+b.level.String())
		}
		tools, ok := trigger.Tools()
		if !ok {
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
