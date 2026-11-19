package domain

// BuildVersion names a build: the release it was cut as and the commit it
// was built from. A release build carries both; a development build may
// carry neither.
type BuildVersion struct {
	Version  string `json:"version,omitempty"`
	Revision string `json:"revision,omitempty"`
}

// IsZero reports a build that names nothing.
func (b BuildVersion) IsZero() bool { return b.Version == "" && b.Revision == "" }
