package application

import (
	"context"
	"testing"
	"time"

	"github.com/underpass-ai/axlr-bots/domain"
)

// Every bot writer waits for the state lock: while another holds it,
// none of them reads the bot (QA L12: a patch and a pause at once lost
// one of the two writes).
func TestSerialRunnerBotWritersWaitForTheStateLock(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	bot := enabledBot(t, h)
	runner := NewSerialRunner(h.deps)
	mission := "Watch the queue."
	writers := map[string]func() error{
		"update": func() error { _, err := runner.UpdateBot(ctx, bot.ID(), BotPatch{Mission: &mission}); return err },
		"pause":  func() error { _, err := runner.PauseBot(ctx, bot.ID()); return err },
		"enable": func() error { _, err := runner.EnableBot(ctx, bot.ID()); return err },
		"stop":   func() error { _, err := runner.StopBot(ctx, bot.ID()); return err },
		"timers": func() error { _, err := runner.TickTimers(ctx); return err },
		"register": func() error {
			d := sampleDefinition()
			d.Name = "worker-2"
			_, err := runner.RegisterBot(ctx, d)
			return err
		},
		"delete": func() error { _, err := runner.DeleteBot(ctx, bot.ID()); return err },
	}
	for _, name := range []string{"update", "pause", "enable", "stop", "timers", "register", "delete"} {
		runner.mu.Lock()
		done := make(chan error, 1)
		go func() { done <- writers[name]() }()
		select {
		case err := <-done:
			t.Fatalf("%s ran while the lock was held: %v", name, err)
		case <-time.After(20 * time.Millisecond):
		}
		runner.mu.Unlock()
		if err := <-done; err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := h.bots.Get(ctx, bot.ID()); !domain.IsNotFound(err) {
		t.Errorf("the bot is still there: %v", err)
	}
}
