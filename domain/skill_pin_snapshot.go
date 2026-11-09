package domain

// SkillPinSnapshot is a catalog skill pin as plain data: the shape a bot
// definition, a task and AXLR's --skills file all use.
type SkillPinSnapshot struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Catalog string `json:"catalog"`
}
