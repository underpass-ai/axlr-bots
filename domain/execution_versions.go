package domain

// ExecutionVersions is what ran an execution, recorded when it starts:
// the version of the bot's definition, the control plane's build, and,
// when a node runs it, the node's build and the AXLR the node reported
// when it registered. A build's Revision is the proof of its version:
// the commit its release tag names.
type ExecutionVersions struct {
	Bot          string        `json:"bot,omitempty"`
	ControlPlane BuildVersion  `json:"controlPlane"`
	Node         *BuildVersion `json:"node,omitempty"`
	AXLR         string        `json:"axlr,omitempty"`
}

// maxVersionField bounds each recorded value: a version line or a commit.
const maxVersionField = 200

// IsZero reports versions never recorded (an execution kept before
// they were).
func (v ExecutionVersions) IsZero() bool {
	return v.Bot == "" && v.ControlPlane.IsZero() && v.Node == nil && v.AXLR == ""
}

// Proven reports whether the build that ran the execution names its
// commit: the node's when a node ran it, the control plane's when it ran
// in process.
func (v ExecutionVersions) Proven() bool {
	if v.Node != nil {
		return v.Node.Revision != ""
	}
	return v.ControlPlane.Revision != ""
}

func (v ExecutionVersions) validate() error {
	fields := map[string]string{"versions.bot": v.Bot, "versions.controlPlane.version": v.ControlPlane.Version,
		"versions.controlPlane.revision": v.ControlPlane.Revision, "versions.axlr": v.AXLR}
	if v.Node != nil {
		fields["versions.node.version"] = v.Node.Version
		fields["versions.node.revision"] = v.Node.Revision
	}
	for field, value := range fields {
		if len(value) > maxVersionField {
			return invalid(field, "must be at most 200 bytes")
		}
	}
	return nil
}
