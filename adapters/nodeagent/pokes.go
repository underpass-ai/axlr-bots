package nodeagent

import (
	"sync"

	"github.com/underpass-ai/axlr-bots/domain"
)

// pokes wakes the lease renewal of a running execution before its tick:
// the control plane pushed that the execution was cancelled, or that a
// message reached it.
type pokes struct {
	mu      sync.Mutex
	running map[domain.ExecutionID]chan struct{}
}

func newPokes() *pokes { return &pokes{running: map[domain.ExecutionID]chan struct{}{}} }

// add answers the channel the execution's renewal listens to.
func (p *pokes) add(execution domain.ExecutionID) <-chan struct{} {
	p.mu.Lock()
	defer p.mu.Unlock()
	poke := make(chan struct{}, 1)
	p.running[execution] = poke
	return poke
}

func (p *pokes) remove(execution domain.ExecutionID) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.running, execution)
}

// poke wakes the execution's renewal; one already woken stays woken once.
func (p *pokes) poke(execution domain.ExecutionID) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if poke, ok := p.running[execution]; ok {
		select {
		case poke <- struct{}{}:
		default:
		}
	}
}

// pokeAll wakes every running execution's renewal.
func (p *pokes) pokeAll() {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, poke := range p.running {
		select {
		case poke <- struct{}{}:
		default:
		}
	}
}
