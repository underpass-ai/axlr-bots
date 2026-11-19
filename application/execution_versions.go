package application

import "github.com/underpass-ai/axlr-bots/domain"

// versionsOf is what runs an execution that starts now: the bot's
// definition version, this control plane's build and, when a node runs
// it, the node's build and AXLR as the node reported them when it
// registered (a node registers again whenever it starts).
func versionsOf(deps Dependencies, bot domain.Bot, node *domain.Node) domain.ExecutionVersions {
	v := domain.ExecutionVersions{Bot: bot.Version().String()}
	if deps.Build != nil {
		running := deps.Build.Running()
		v.ControlPlane = domain.BuildVersion{Version: running.Version, Revision: running.Revision}
	}
	if node != nil {
		c := node.Capabilities()
		v.Node = &domain.BuildVersion{Version: c.NodeVersion, Revision: c.NodeRevision}
		v.AXLR = c.AXLRVersion
	}
	return v
}
