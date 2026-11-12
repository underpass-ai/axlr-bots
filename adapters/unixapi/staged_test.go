package unixapi

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

// The staged routes behind the token: stage a change naming its version,
// see it on the bot (version and time only) and whole on its route (its
// secrets redacted), apply it, and the refusals when nothing is staged.
func TestStagedRoutesOverTheOperatorAddress(t *testing.T) {
	o := serveOperatorAddress(t)
	ctx := context.Background()
	definition := triggered("bot-s")
	definition.Version = "1.0.0"
	registered, err := o.api.RegisterBot(ctx, definition)
	if err != nil || registered.Version != "1.0.0" || registered.Staged != nil {
		t.Fatalf("registered %+v %v", registered, err)
	}
	mission := "Watch the deploys and say what failed."
	triggers := []domain.TriggerSnapshot{{Type: "manual"}, {Type: "webhook", Name: "deploy", Objective: "Handle {{.type}}", Secret: testSecret}}
	patch := application.BotPatch{Version: ptrTo("1.1.0"), Mission: &mission, Triggers: &triggers}

	var unauthorized UnauthorizedError
	for name, try := range map[string]func() error{
		"stage":   func() error { _, err := o.noToken.StageBot(ctx, "bot-s", patch); return err },
		"show":    func() error { _, err := o.noToken.GetStagedBot(ctx, "bot-s"); return err },
		"apply":   func() error { _, err := o.noToken.ApplyStagedBot(ctx, "bot-s"); return err },
		"unstage": func() error { _, err := o.noToken.UnstageBot(ctx, "bot-s"); return err },
	} {
		if err := try(); !errors.As(err, &unauthorized) {
			t.Errorf("%s without the token: %v", name, err)
		}
	}
	if _, err := o.api.StageBot(ctx, "bot-s", application.BotPatch{Mission: &mission}); err == nil || !strings.Contains(err.Error(), "version") {
		t.Errorf("a staged change without its version: %v", err)
	}
	staged, err := o.api.StageBot(ctx, "bot-s", patch)
	if err != nil {
		t.Fatal(err)
	}
	if staged.Version != "1.0.0" || staged.Staged == nil || staged.Staged.Version != "1.1.0" || staged.Staged.StagedAt.IsZero() || staged.Staged.Change != nil || staged.Mission == mission {
		t.Fatalf("staged answer %+v", staged)
	}
	if listed, err := o.api.ListBots(ctx); err != nil || listed[0].Staged == nil || listed[0].Staged.Change != nil {
		t.Fatalf("listed %+v %v", listed, err)
	}
	view, err := o.api.GetStagedBot(ctx, "bot-s")
	if err != nil || view.Version != "1.1.0" || view.Patch.Mission == nil || *view.Patch.Mission != mission || (*view.Patch.Triggers)[1].Secret != domain.Redacted {
		t.Fatalf("view %+v %v", view, err)
	}

	applied, err := o.api.ApplyStagedBot(ctx, "bot-s")
	if err != nil || applied.Version != "1.1.0" || applied.Staged != nil || applied.Mission != mission {
		t.Fatalf("applied %+v %v", applied, err)
	}
	stored, _ := o.local.Dependencies().Bots.Get(ctx, "bot-s")
	if hook, ok := stored.Trigger("deploy"); !ok || hook.Snapshot().Secret != testSecret {
		t.Error("the applied trigger keeps its secret in the store")
	}
	events, err := o.api.ListEvents(ctx, "bot-s", 0)
	if err != nil {
		t.Fatal(err)
	}
	var types []string
	for _, event := range events {
		types = append(types, event.Type.String())
	}
	if last := events[len(events)-1]; last.Type != domain.EventBotUpdated || last.Payload["staged"] != "1.1.0" {
		t.Errorf("the apply is the update: %+v", last)
	}
	if joined := strings.Join(types, ","); strings.Count(joined, "bot.updated") != 1 {
		t.Errorf("events %s", joined)
	}

	for name, try := range map[string]func() error{
		"apply":   func() error { _, err := o.api.ApplyStagedBot(ctx, "bot-s"); return err },
		"unstage": func() error { _, err := o.api.UnstageBot(ctx, "bot-s"); return err },
		"show":    func() error { _, err := o.api.GetStagedBot(ctx, "bot-s"); return err },
	} {
		var remote RemoteError
		if err := try(); !errors.As(err, &remote) || remote.Kind != KindState {
			t.Errorf("%s with nothing staged: %v", name, err)
		}
	}
	request, _ := http.NewRequest(http.MethodPost, o.url+"/v1/bots/bot-s/staged/apply", nil)
	request.Header.Set("Authorization", "Bearer "+testToken)
	if response, err := o.http.Do(request); err != nil || response.StatusCode != http.StatusConflict {
		t.Errorf("apply with nothing staged: %v %v", response, err)
	}

	if _, err := o.api.StageBot(ctx, "bot-s", application.BotPatch{Version: ptrTo("1.2.0"), Mission: &mission}); err != nil {
		t.Fatal(err)
	}
	unstaged, err := o.api.UnstageBot(ctx, "bot-s")
	if err != nil || unstaged.Staged != nil || unstaged.Version != "1.1.0" {
		t.Fatalf("unstaged %+v %v", unstaged, err)
	}
	if _, err := o.api.ApplyStagedBot(ctx, "nobody"); !domain.IsNotFound(err) {
		t.Errorf("an unknown bot: %v", err)
	}
	for name, try := range map[string]func() error{
		"stage":   func() error { _, err := o.local.StageBot(ctx, "Bad Id", patch); return err },
		"show":    func() error { _, err := o.local.GetStagedBot(ctx, "Bad Id"); return err },
		"apply":   func() error { _, err := o.local.ApplyStagedBot(ctx, "Bad Id"); return err },
		"unstage": func() error { _, err := o.local.UnstageBot(ctx, "Bad Id"); return err },
	} {
		if err := try(); err == nil {
			t.Errorf("%s took a bad id", name)
		}
	}
	// A body the update would refuse is refused before anything is read.
	put, _ := http.NewRequest(http.MethodPut, o.url+"/v1/bots/bot-s/staged", strings.NewReader(`{"version":"1.3.0","colour":"blue"}`))
	put.Header.Set("Authorization", "Bearer "+testToken)
	response, err := o.http.Do(put)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if response.StatusCode != http.StatusBadRequest {
		t.Errorf("an unknown field: %d %s", response.StatusCode, raw)
	}
}

func ptrTo(s string) *string { return &s }
