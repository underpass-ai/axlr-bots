package domain

import "regexp"

// The shapes AXLR accepts for a pin (AXLR tui/domain/execution_skills.go).
// The version is exact: no range, no latest, no leading v.
var (
	skillCatalogPattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,31}$`)
	skillNamePattern    = regexp.MustCompile(`^[a-z][a-z0-9_]{1,47}$`)
	skillVersionPattern = regexp.MustCompile(`^(0|[1-9][0-9]{0,8})\.(0|[1-9][0-9]{0,8})\.(0|[1-9][0-9]{0,8})$`)
)

// SkillPin is one signed catalog skill at one exact version, as a bot is
// given it: the catalog is named, never inferred, so a same-named skill
// of another catalog cannot take its place.
type SkillPin struct {
	name    string
	version string
	catalog string
}

// NewSkillPin validates a pin.
func NewSkillPin(s SkillPinSnapshot) (SkillPin, error) {
	if !skillNamePattern.MatchString(s.Name) {
		return SkillPin{}, invalid("skills", "name "+quoted(s.Name)+" must be 2-48 lowercase letters, digits or underscores, starting with a letter")
	}
	if !skillVersionPattern.MatchString(s.Version) {
		return SkillPin{}, invalid("skills", s.Name+": version "+quoted(s.Version)+" must be one exact major.minor.patch, such as 1.0.0: no range and no latest")
	}
	if !skillCatalogPattern.MatchString(s.Catalog) {
		return SkillPin{}, invalid("skills", s.Name+": catalog "+quoted(s.Catalog)+" must be a configured catalog's name")
	}
	return SkillPin{name: s.Name, version: s.Version, catalog: s.Catalog}, nil
}

// Name is the skill's name in its catalog.
func (p SkillPin) Name() string { return p.name }

// Version is the exact version.
func (p SkillPin) Version() string { return p.version }

// Catalog is the catalog that signs it.
func (p SkillPin) Catalog() string { return p.catalog }

// Key names the pin as AXLR prints it: catalog/name@version.
func (p SkillPin) Key() string { return p.catalog + "/" + p.name + "@" + p.version }

// Snapshot is the pin as plain data.
func (p SkillPin) Snapshot() SkillPinSnapshot {
	return SkillPinSnapshot{Name: p.name, Version: p.version, Catalog: p.catalog}
}

func quoted(s string) string { return "\"" + s + "\"" }
