package domain

import "regexp"

// semanticVersionPattern is Semantic Versioning 2.0.0: MAJOR.MINOR.PATCH
// with no leading zero and no leading v, then an optional -prerelease
// and an optional +build.
var semanticVersionPattern = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)` +
	`(-((0|[1-9][0-9]*|[0-9]*[a-zA-Z-][0-9a-zA-Z-]*)(\.(0|[1-9][0-9]*|[0-9]*[a-zA-Z-][0-9a-zA-Z-]*))*))?` +
	`(\+([0-9a-zA-Z-]+(\.[0-9a-zA-Z-]+)*))?$`)

// maxSemanticVersionLength bounds a version, as every other text is.
const maxSemanticVersionLength = 64

// SemanticVersion is a version as Semantic Versioning 2.0.0 writes it,
// such as 1.4.0, 2.0.0-rc.1 or 1.4.0+build.7: the version of a bot's
// definition, of a staged change, the least AXLR the fleet wants. The
// zero value is no version.
type SemanticVersion string

// ParseSemanticVersion validates a version; field names it in the error.
func ParseSemanticVersion(field, raw string) (SemanticVersion, error) {
	if len(raw) > maxSemanticVersionLength || !semanticVersionPattern.MatchString(raw) {
		return "", invalid(field, quoted(raw)+" must be a semantic version, MAJOR.MINOR.PATCH with an optional -prerelease and +build, such as 1.4.0 or 2.0.0-rc.1: no leading v")
	}
	return SemanticVersion(raw), nil
}

// IsZero reports no version.
func (v SemanticVersion) IsZero() bool { return v == "" }

func (v SemanticVersion) String() string { return string(v) }
