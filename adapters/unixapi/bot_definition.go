package unixapi

import "github.com/underpass-ai/axlr-bots/application"

// botDefinition is the register request: a bot definition as the
// example bots in cmd/axlr-bots/testdata write it.
type botDefinition struct {
	application.BotDefinition
}
