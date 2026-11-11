package domain

// A team's Jev watcher (docs/architecture/team-channel.md, A6) is a Jev
// bot whose program names the reserved server below: the node serves its
// tools in process, with the execution's identity, never over HTTP, and
// the control plane answers them for the questions its team asked.
const (
	// WatchServer is the MCP server name reserved for the watcher's
	// tools; no mcp.json entry is read for it.
	WatchServer = "axlr-bots"
	// WatchNextTool starts a round and waits: the next open question of
	// the watcher's team, or done.
	WatchNextTool ToolName = "axlr_bots_watch_next"
	// WatchDecideTool acts on Jev's choice for that question.
	WatchDecideTool ToolName = "axlr_bots_watch_decide"
	// WatchChoice and WatchVerdict are the decide tool's arguments that
	// take Jev's choice and its verdict.
	WatchChoice  = "choice"
	WatchVerdict = "verdict"
	// WatchWait and WatchAbandon are the two options a watcher's question
	// puts to Jev.
	WatchWait    = "wait"
	WatchAbandon = "abandon"
)

// watchProgram refuses a program on the reserved server that is not the
// watcher's: its tools are the two watch tools, in their places, and it
// has no stop tool.
func watchProgram(p JevProgram) error {
	if p.server != WatchServer {
		return nil
	}
	if p.start != WatchNextTool || p.wait != WatchNextTool || p.act != WatchDecideTool || p.stop != "" {
		return invalid("jev server", "axlr-bots is the watcher's: start and wait are "+WatchNextTool.String()+", act is "+WatchDecideTool.String()+" and there is no stop tool")
	}
	if p.choice != WatchChoice || p.verdict != WatchVerdict {
		return invalid("jev act", "on axlr-bots the act takes Jev's choice as "+WatchChoice+" and its verdict as "+WatchVerdict)
	}
	return nil
}

// WatchesQuestions reports whether the program is a team's Jev watcher.
func (p JevProgram) WatchesQuestions() bool { return p.server == WatchServer }
