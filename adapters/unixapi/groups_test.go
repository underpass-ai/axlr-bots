package unixapi

import (
	"context"
	"testing"

	"github.com/underpass-ai/axlr-bots/domain"
)

func TestGroupsOverTheSocket(t *testing.T) {
	client, _, _ := serve(t, "complete")
	ctx := context.Background()
	for _, name := range []string{"plane", "quad-1"} {
		d := definition(name, "observe")
		d.Groups = []string{"port"}
		if _, err := client.RegisterBot(ctx, d); err != nil {
			t.Fatal(err)
		}
	}
	groups, err := client.ListGroups(ctx)
	if err != nil || len(groups) != 1 || groups[0].Name != "port" || len(groups[0].Members) != 2 {
		t.Fatalf("groups = %+v %v", groups, err)
	}
	group, err := client.GetGroup(ctx, "port", "ops")
	if err != nil || group.TenantID != "ops" || group.Members[0].BotID != "plane" {
		t.Fatalf("group = %+v %v", group, err)
	}
	if _, err := client.GetGroup(ctx, "sea", ""); !domain.IsNotFound(err) {
		t.Errorf("missing group: %v", err)
	}
	bot, err := client.GetBot(ctx, "quad-1")
	if err != nil || len(bot.Groups) != 1 {
		t.Errorf("bot groups = %+v %v", bot.Groups, err)
	}
}
