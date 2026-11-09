package application

import (
	"slices"
	"strings"

	"github.com/underpass-ai/axlr-bots/domain"
)

// placementWait says why a Pending task of a placed bot has not started,
// and true, when no node its placement admits is Online with its
// runtime: such a task waits for its node and never runs in the control
// plane's process. The sentence names each node the placement admits and
// what keeps it from the task: "waiting for node web-1, which is
// Offline", "waiting for a node labelled zone=lab: web-2 lacks the jev
// runtime". A bot that takes no task now, a bot with an empty placement
// and a bot a node may run wait for no node, and answer false.
func placementWait(bot domain.Bot, skills domain.SkillPins, nodes []domain.Node) (string, bool) {
	placement := bot.Placement()
	if placement.IsEmpty() || !bot.AcceptsTasks() || hasOnlineNode(bot, skills, nodes) {
		return "", false
	}
	registered := make(map[domain.NodeID]domain.Node, len(nodes))
	for _, node := range nodes {
		registered[node.ID()] = node
	}
	named := placement.Nodes()
	var states []string
	for _, id := range named {
		node, found := registered[id]
		states = append(states, id.String()+" "+keptBy(bot, node, found))
	}
	head := "waiting for " + placement.Wanted()
	if len(placement.Selector()) == 0 {
		if len(named) == 1 {
			node, found := registered[named[0]]
			return head + ", which " + keptBy(bot, node, found), true
		}
		return head + ": " + strings.Join(states, ", "), true
	}
	matched := false
	for _, node := range nodes {
		if slices.Contains(named, node.ID()) || !placement.Admits(node) {
			continue
		}
		matched = true
		states = append(states, node.ID().String()+" "+keptBy(bot, node, true))
	}
	if !matched {
		states = append(states, "no node has these labels")
	}
	return head + ": " + strings.Join(states, ", "), true
}

// keptBy says what keeps an admitted node from the bot's task: it is not
// registered, not Online, or Online without the bot's runtime or without
// a way to read the task's skills.
func keptBy(bot domain.Bot, node domain.Node, found bool) string {
	kind := bot.Runtime().Kind()
	switch {
	case !found:
		return "is not registered"
	case node.IsOnline() && !node.Capabilities().Runs(kind):
		return "lacks the " + kind.String() + " runtime"
	case node.IsOnline():
		return "has an axlr-tui that takes no pinned skills"
	case node.Status() == domain.NodeRegistered:
		return "is Registered but not yet Online"
	}
	return "is " + node.Status().String()
}
