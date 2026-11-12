// Package githubhooks is the input adapter that turns GitHub's webhooks
// into the dev team's events (docs/architecture/team-topics.md, «Where
// GitHub's facts come from»): the DTOs generated from
// docs/reference/github-hooks.asyncapi.json, the mappers from them to
// application.PublishGroupEventCommand, the HTTP handler that verifies a
// delivery's signature and publishes through a port, and the redelivery
// of the failed deliveries through GitHub's REST API, behind a port too.
// It is a process beside the world (docs/architecture/input-adapters.md):
// it publishes as the operator and decides nothing.
package githubhooks

//go:generate go run ../../internal/asyncapigen/cmd/asyncapigen -spec ../../docs/reference/github-hooks.asyncapi.json -package githubhooks -out .
