package unixapi

import (
	"context"
	"testing"

	"github.com/underpass-ai/axlr-bots/adapters/bus/bustest"
	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

// The direct adapter, the node's stream over its own connection to the
// control plane, meets the bus contract.
func TestTheDirectStreamMeetsTheBusContract(t *testing.T) {
	bustest.Run(t, func(t *testing.T) (application.NodePusher, bustest.Open) {
		client, local, _ := serve(t, "completed")
		for _, node := range []domain.NodeID{bustest.NodeA, bustest.NodeB} {
			if _, err := client.Register(context.Background(), application.NodeRegistration{ID: node.String(), HostName: node.String()}); err != nil {
				t.Fatal(err)
			}
		}
		return local.Pusher(), client.Stream
	})
}
