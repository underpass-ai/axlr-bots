package domain

// ChannelSide is which side of a question a team's store keeps
// (docs/architecture/team-channel.md, "Memory"): the team that asked, or
// the team that answered.
type ChannelSide string

const (
	// ChannelAsked is the asking team's record: it asked, and what came
	// back.
	ChannelAsked ChannelSide = "asked"
	// ChannelAnswered is the answering team's record: what it was asked,
	// and what it answered.
	ChannelAnswered ChannelSide = "answered"
)

// ParseChannelSide reads a side: asked or answered.
func ParseChannelSide(raw string) (ChannelSide, error) {
	switch side := ChannelSide(raw); side {
	case ChannelAsked, ChannelAnswered:
		return side, nil
	}
	return "", invalid("channel side", "must be asked or answered")
}

func (s ChannelSide) String() string { return string(s) }
