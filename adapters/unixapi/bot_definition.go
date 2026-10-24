package unixapi

import "github.com/underpass-ai/underpass-bots/application"

// botDefinition is the register request: a bot definition as the
// examples write it.
type botDefinition struct {
	application.BotDefinition
}
