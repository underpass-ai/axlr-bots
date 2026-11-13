package transcriptaudit

import "context"

// Judge is the port Jev answers through: one choice among the options for
// one state (the composition root wires adapters/jev's client).
type Judge interface {
	Choose(ctx context.Context, choice Choice) (Judgement, error)
}
