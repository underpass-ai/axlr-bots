package application

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/underpass-ai/axlr-bots/domain"
)

func TestATeamsLinksNeedBothSides(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	asks := func(teams ...string) *domain.AskSnapshot { return &domain.AskSnapshot{Teams: teams} }
	channelBot(t, h, "qa-jefe", []string{"qa"}, asks("dev"), []string{"dev"})
	channelBot(t, h, "qa-cola", []string{"qa"}, asks("dev", "marketing"), nil)
	channelBot(t, h, "dev-b", []string{"dev"}, nil, []string{"qa"})
	channelBot(t, h, "dev-a", []string{"dev"}, asks("qa"), []string{"qa", "producto"})
	channelBot(t, h, "dev-revisor", []string{"dev"}, nil, nil)
	// Its memory team is dev, the first of its teams by name.
	channelBot(t, h, "dev-zeta", []string{"zeta", "dev"}, nil, []string{"qa"})
	// Marketing asks dev, and nobody in dev answers marketing: no link.
	channelBot(t, h, "mkt-redactor", []string{"marketing"}, asks("dev"), nil)
	// Another tenant's bot that answers qa is no responder of this one.
	other := sampleDefinition()
	other.ID, other.Name, other.Tenant = "dev-otro", "dev-otro", "otro"
	other.Groups = []string{"dev"}
	other.Triggers = []domain.TriggerSnapshot{{Type: "question", From: []string{"qa"}}}
	if _, err := NewRegisterBot(h.deps).Execute(ctx, other); err != nil {
		t.Fatal(err)
	}

	links, err := NewListGroupLinks(h.deps).Execute(ctx, "dev", "default")
	if err != nil {
		t.Fatal(err)
	}
	want := []GroupLink{
		{From: "dev", To: "qa", Askers: []string{"dev-a"}, Responders: []string{"qa-jefe"}},
		{From: "qa", To: "dev", Askers: []string{"qa-cola", "qa-jefe"}, Responders: []string{"dev-a", "dev-b", "dev-zeta"}},
	}
	if !reflect.DeepEqual(links, want) {
		t.Errorf("dev's links = %+v", links)
	}
	qa, err := NewListGroupLinks(h.deps).Execute(ctx, "qa", "")
	if err != nil || !reflect.DeepEqual(qa, want) {
		t.Errorf("qa's links = %+v %v", qa, err)
	}
	if marketing, err := NewListGroupLinks(h.deps).Execute(ctx, "marketing", ""); err != nil || marketing == nil || len(marketing) != 0 {
		t.Errorf("marketing's links = %#v %v", marketing, err)
	}
	if zeta, err := NewListGroupLinks(h.deps).Execute(ctx, "zeta", "default"); err != nil || len(zeta) != 0 {
		t.Errorf("a team nobody remembers in = %+v %v", zeta, err)
	}
	var missing domain.NotFoundError
	if _, err := NewListGroupLinks(h.deps).Execute(ctx, "nadie", ""); !errors.As(err, &missing) {
		t.Errorf("an unknown team = %v", err)
	}
	var ambiguous domain.ValidationError
	if _, err := NewListGroupLinks(h.deps).Execute(ctx, "dev", ""); !errors.As(err, &ambiguous) {
		t.Errorf("a team of two tenants without one named = %v", err)
	}
}
