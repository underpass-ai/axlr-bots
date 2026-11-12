package domain

// BuildIdentity is the source a binary was built from, as Go records it
// at build time: the module version, the commit, its time and whether
// the tree had changes. A build without version control information
// (go run, a test binary) has no revision and is not Known: nothing can
// be said about it.
type BuildIdentity struct {
	Version  string
	Revision string
	Time     string
	Modified bool
}

// Known says the build names its commit.
func (b BuildIdentity) Known() bool { return b.Revision != "" }

// Matches says two builds come from the same commit in the same state.
func (b BuildIdentity) Matches(other BuildIdentity) bool {
	return b.Revision == other.Revision && b.Modified == other.Modified
}
