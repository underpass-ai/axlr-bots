package nodeagent

import (
	"context"
	"time"

	"github.com/underpass-ai/axlr-bots/domain"
)

// channelMemoryTimeout bounds one write of the team channel's record,
// the KMP server's start included.
const channelMemoryTimeout = 30 * time.Second

// remember writes a team channel's record into its team's store on this
// node (docs/architecture/team-channel.md, "Memory"), when the node
// writes the channel's memory (Agent.Memory, --team-channel-memory) and
// shares that team's memory (--memory-teams): the store the team's bots
// open here. It is best-effort and never fails what the record came
// with: an error is logged. It has its own context, so a node that stops
// still writes what it was handed.
func (a Agent) remember(record *domain.ChannelRecord, from string) {
	if a.Memory == nil || record == nil {
		return
	}
	logger := a.Options.log()
	if !(domain.NodeCapabilities{MemoryTeams: a.Options.MemoryTeams}).SharesMemoryOf(record.Team) {
		logger.Printf("node: team channel: question %s (%s, from %s) not written: this node does not share the memory of team %s", record.Question, record.Side, from, record.Team)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), channelMemoryTimeout)
	defer cancel()
	if err := a.Memory.WriteChannelRecord(ctx, *record); err != nil {
		logger.Printf("node: team channel: question %s (%s, from %s) not written into kmp/%s: %v", record.Question, record.Side, from, record.Team, err)
		return
	}
	logger.Printf("node: team channel: question %s (%s, from %s) written into kmp/%s, about %s", record.Question, record.Side, from, record.Team, record.About())
}

// goRemember writes the record without holding up its caller (a run that
// starts, a lease that renews); the agent waits for it before it stops.
func (a Agent) goRemember(record *domain.ChannelRecord, from string) {
	if a.Memory == nil || record == nil {
		return
	}
	if a.wg == nil {
		a.remember(record, from)
		return
	}
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		a.remember(record, from)
	}()
}
