package main

import (
	"os"
	"testing"
)

// TestMain makes the HTTP transport the default of the end-to-end tests
// that start the daemon's HTTP API; the gRPC commands are tested with
// --transport grpc against in-process servers.
func TestMain(m *testing.M) {
	if os.Getenv("AXLR_BOTS_TRANSPORT") == "" {
		os.Setenv("AXLR_BOTS_TRANSPORT", transportREST)
	}
	os.Exit(m.Run())
}
