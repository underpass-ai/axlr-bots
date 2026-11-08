package domain

// LadderUnit is the runbook and the namespace an incident's signals and
// apply fall on: one runbook in one namespace.
type LadderUnit struct {
	runbook   LadderRunbook
	namespace string
	ladder    Ladder
}

// Runbook, Namespace and Thresholds name the unit.
func (u LadderUnit) Runbook() LadderRunbook         { return u.runbook }
func (u LadderUnit) Namespace() string              { return u.namespace }
func (u LadderUnit) Thresholds() AutonomyThresholds { return u.ladder.thresholds }

// AutonomyID is the record of the unit, on the bot and tenant given.
func (u LadderUnit) AutonomyID(tenant TenantID, bot BotID) AutonomyID {
	return AutonomyIDOf(tenant, bot, u.runbook.name, u.namespace)
}
