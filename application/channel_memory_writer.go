package application

import (
	"context"

	"github.com/underpass-ai/axlr-bots/domain"
)

// ChannelMemoryWriter writes a team channel's record into its team's
// store on a node, without a model (docs/architecture/team-channel.md,
// "Memory"). The node calls it best-effort: an error is logged and never
// fails the task the record came with.
type ChannelMemoryWriter interface {
	WriteChannelRecord(ctx context.Context, record domain.ChannelRecord) error
}
