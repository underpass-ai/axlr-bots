package domain

import "strings"

// Bounds of what an execution's artifacts may hold, as AXLR bounds a
// delivery: a file of at most 32 MiB, and 64 artifacts per execution.
const (
	MaxArtifactBytes         = 32 << 20
	MaxArtifactsPerExecution = 64
	maxArtifactNameLength    = 128
)

// ValidateArtifactName accepts the names AXLR gives a delivery: 1 to 128
// letters, digits, '.', '_' and '-', starting with a letter or a digit,
// with no "..": a plain file name, never a path.
func ValidateArtifactName(name string) error {
	if name == "" || len(name) > maxArtifactNameLength {
		return invalid("artifact name", "must be 1 to 128 characters")
	}
	first := name[0]
	if !isASCIILetterOrDigit(first) {
		return invalid("artifact name", "must start with a letter or a digit")
	}
	if strings.Contains(name, "..") {
		return invalid("artifact name", "must not contain ..")
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		if !isASCIILetterOrDigit(c) && c != '.' && c != '_' && c != '-' {
			return invalid("artifact name", "may only use letters, digits, '.', '_' and '-'")
		}
	}
	return nil
}

func isASCIILetterOrDigit(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}
