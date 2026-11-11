package nodeagent

import (
	"slices"
	"strings"

	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

// channelTools are the tools the node serves for the team channel to an
// asking run: asking a team and reading a question.
var channelTools = []string{application.AskToolName, application.QuestionStatusToolName}

// askOffered is the ask offer the node serves: the control plane's, or
// none when the node runs with the team channel off (--team-channel),
// whatever the control plane offered. The control plane offers such a
// node none, so this only holds the line against one that would.
func (a Agent) askOffered(execution domain.ExecutionID, ask *application.AskOffer) *application.AskOffer {
	if ask == nil || a.Options.TeamChannel {
		return ask
	}
	a.Options.log().Printf("node: execution %s: the team channel is off on this node (--team-channel): no %s", execution, strings.Join(channelTools, ", "))
	return nil
}

// allowedWithout is the run's allowed tools less the team channel's when
// the node does not serve them.
func (a Agent) allowedWithout(tools []string) []string {
	if a.Options.TeamChannel {
		return tools
	}
	return slices.DeleteFunc(slices.Clone(tools), func(tool string) bool { return slices.Contains(channelTools, tool) })
}

// servesWatcher reports whether the node serves a team's Jev watcher its
// in-process tools: only with the team channel on. Off, the watcher's run
// finds no node tool and fails, as the control plane would refuse it.
func (a Agent) servesWatcher(execution domain.ExecutionID) bool {
	if a.Options.TeamChannel {
		return true
	}
	a.Options.log().Printf("node: execution %s: a team's watcher, but the team channel is off on this node (--team-channel): no watch tools", execution)
	return false
}
