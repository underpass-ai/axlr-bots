package domain

// ExecutionPolicy is the level, the tool allow-list and its rules one
// task's executions run under: the bot's.
type ExecutionPolicy struct {
	level AutonomyLevel
	tools AllowedTools
	rules ToolRules
}

// Level is the autonomy level the executions run at.
func (p ExecutionPolicy) Level() AutonomyLevel { return p.level }

// Tools is the allow-list the executions run with.
func (p ExecutionPolicy) Tools() AllowedTools { return p.tools }

// Rules is the bot's tool rules within the allow-list.
func (p ExecutionPolicy) Rules() ToolRules { return p.rules }

// Policy is the policy the bot's tasks run under: its level, its
// allow-list and its tool rules.
func (b Bot) Policy() ExecutionPolicy {
	return ExecutionPolicy{level: b.level, tools: b.allowedTools, rules: b.toolRules}
}
