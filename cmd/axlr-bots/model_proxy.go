package main

import (
	"fmt"
	"io"
	"os"

	"github.com/underpass-ai/axlr-bots/adapters/jev"
	"github.com/underpass-ai/axlr-bots/adapters/openrouter"
	"github.com/underpass-ai/axlr-bots/application"
)

// withModelProxy gives the control plane the keys it answers a keyless
// node's calls with, the ones in its own environment; a key that is
// absent or bad leaves that route refusing.
func withModelProxy(app application.Dependencies, stderr io.Writer) application.Dependencies {
	if key := os.Getenv("OPENROUTER_API_KEY"); key != "" {
		client, err := openrouter.NewClient(key, "")
		if err != nil {
			fmt.Fprintln(stderr, "axlr-bots: model proxy:", err)
		} else {
			app.ModelGateway = client
		}
	}
	if key := os.Getenv("TYPESAFE_API_KEY"); key != "" {
		client, err := jev.NewClient(jev.ClientConfig{APIKey: key})
		if err != nil {
			fmt.Fprintln(stderr, "axlr-bots: Jev proxy:", err)
		} else {
			app.JudgeGateway = jev.Gateway{Client: client}
		}
	}
	return app
}
