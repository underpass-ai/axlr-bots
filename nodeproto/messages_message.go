package nodeproto

import "github.com/underpass-ai/axlr-bots/application"

// MessagesMessage is the operator's messages for an execution, as its
// node reads them: oldest first, after the cursor the node sent.
type MessagesMessage struct {
	Messages []application.RelayedMessage `json:"messages"`
}
