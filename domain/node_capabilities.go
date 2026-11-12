package domain

// NodeCapabilities is what a node can run: the axlr-tui it has, whether
// that build takes an execution policy and the correlation ids, and
// whether the node is dedicated: it runs only the bots whose placement
// names it or selects its labels, never those placed on any node (a
// sidecar beside one service), and the runtimes it has beside AXLR (a
// node that names none runs AXLR bots only), and the model providers its
// bots can reach, by name only (`openrouter`, `codex`, `jev`, `local:<id>`
// for each local model its AXLR names): never a key, nor where it is kept.
// A node older than the field names none, which says nothing.
// SkillsSupported says its axlr-tui takes pinned catalog skills: a task
// with skills is given to no other node, since an older one would ignore
// them and run without. MemoryTeams are the teams whose bots share one
// KMP store on the node (its --memory-teams; `*` for every team): only
// such a node is picked to answer a team's question, and an older node
// that names none never is. TeamChannel says the node runs the team
// channel (axlr-node --team-channel, off by default): only then are its
// runs offered the ask tool, its bots picked to answer, and a watcher
// served; an older node reports none, which is off. NodeVersion and
// NodeRevision are the node agent's own build, as Go recorded it (the
// module version, the commit): set beside the control plane's own
// (GET /v1/build) they say a node runs an older agent; an older node
// names none, which says nothing.
type NodeCapabilities struct {
	AXLRVersion          string   `json:"axlrVersion,omitempty"`
	NodeVersion          string   `json:"nodeVersion,omitempty"`
	NodeRevision         string   `json:"nodeRevision,omitempty"`
	PolicySupported      bool     `json:"policySupported"`
	CorrelationSupported bool     `json:"correlationSupported"`
	Dedicated            bool     `json:"dedicated,omitempty"`
	Runtimes             []string `json:"runtimes,omitempty"`
	Providers            []string `json:"providers,omitempty"`
	SkillsSupported      bool     `json:"skillsSupported,omitempty"`
	MemoryTeams          []string `json:"memoryTeams,omitempty"`
	TeamChannel          bool     `json:"teamChannel,omitempty"`
}

// AllMemoryTeams is what a node reports in MemoryTeams when every team's
// bots share their team's store on it.
const AllMemoryTeams = "*"

// SharesMemoryOf reports whether the node opens the team's store for
// the team's bots: it named the team, or every team.
func (c NodeCapabilities) SharesMemoryOf(team TeamName) bool {
	if team == "" {
		return false
	}
	for _, name := range c.MemoryTeams {
		if name == AllMemoryTeams || name == team.String() {
			return true
		}
	}
	return false
}

// AnswersFor reports whether the node may answer the team's questions:
// it runs the team channel and opens the team's store.
func (c NodeCapabilities) AnswersFor(team TeamName) bool {
	return c.TeamChannel && c.SharesMemoryOf(team)
}

// HasProvider reports whether the node named the provider.
func (c NodeCapabilities) HasProvider(name string) bool {
	for _, provider := range c.Providers {
		if provider == name {
			return true
		}
	}
	return false
}

// Runs reports whether the node runs bots of the runtime: every node
// runs AXLR; another runtime only when the node named it.
func (c NodeCapabilities) Runs(kind RuntimeKind) bool {
	if kind == RuntimeAXLR {
		return true
	}
	for _, runtime := range c.Runtimes {
		if runtime == kind.String() {
			return true
		}
	}
	return false
}
