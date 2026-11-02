package filestore

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

// Each update reads the last write: 50 at once, each moving the node's
// last sighting one second on, leave it 50 seconds on, whichever
// repository on the state directory made them.
func TestNodeUpdatesDoNotLoseOneAnother(t *testing.T) {
	state := t.TempDir()
	ctx := context.Background()
	node, _ := domain.NewNode("web-1", "web-1", nil, domain.NodeCapabilities{}, t0)
	if err := NewNodeRepository(state).Save(ctx, node); err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	var wg sync.WaitGroup
	for range 50 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			err := NewNodeRepository(state).Update(ctx, "web-1", func(n *domain.Node) error {
				return n.Heartbeat(n.LastSeenAt().Add(time.Second))
			})
			if err != nil {
				t.Error(err)
			}
		}()
	}
	close(start)
	wg.Wait()
	back, err := NewNodeRepository(state).Get(ctx, "web-1")
	if err != nil || !back.LastSeenAt().Equal(t0.Add(50*time.Second)) {
		t.Errorf("last seen = %v %v, want %v", err, back.LastSeenAt(), t0.Add(50*time.Second))
	}
}

func TestNodeUpdateWritesOnlyAChange(t *testing.T) {
	state := t.TempDir()
	ctx := context.Background()
	nodes := NewNodeRepository(state)
	called := false
	if err := nodes.Update(ctx, "ghost", func(*domain.Node) error { called = true; return nil }); !domain.IsNotFound(err) || called {
		t.Errorf("unknown node: %v, change called %v", err, called)
	}
	node, _ := domain.NewNode("web-1", "web-1", nil, domain.NodeCapabilities{}, t0)
	_ = nodes.Save(ctx, node)
	path := filepath.Join(state, "nodes", "web-1.json")
	before, _ := os.Stat(path)
	if err := nodes.Update(ctx, "web-1", func(*domain.Node) error { return application.ErrNodeUnchanged }); err != nil {
		t.Errorf("unchanged: %v", err)
	}
	refused := errors.New("refused")
	if err := nodes.Update(ctx, "web-1", func(n *domain.Node) error { _ = n.Heartbeat(t0.Add(time.Hour)); return refused }); !errors.Is(err, refused) {
		t.Errorf("refused: %v", err)
	}
	after, _ := os.Stat(path)
	if !after.ModTime().Equal(before.ModTime()) {
		t.Error("an update that changed nothing wrote the node")
	}
	if back, _ := nodes.Get(ctx, "web-1"); back.IsOnline() {
		t.Errorf("a refused change was written: %+v", back.Snapshot())
	}
}
