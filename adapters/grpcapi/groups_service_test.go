package grpcapi

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"google.golang.org/protobuf/types/known/structpb"

	operatorv1 "github.com/underpass-ai/axlr-bots/api/gen/axlrbots/operator/v1"
)

func opsTeam(t *testing.T, f *fixture) {
	t.Helper()
	bots := operatorv1.NewBotsServiceClient(f.authed)
	for _, name := range []string{"watcher", "worker-1"} {
		d := definition(name, "observe")
		d.Groups = []string{"ops"}
		d.Events = &operatorv1.Events{Emit: []*operatorv1.EmitRule{{Name: "warn.deploy-slow"}}}
		if _, err := bots.RegisterBot(context.Background(), &operatorv1.RegisterBotRequest{Definition: d}); err != nil {
			t.Fatal(err)
		}
		if _, err := bots.EnableBot(context.Background(), &operatorv1.EnableBotRequest{BotId: name}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestGroupsServiceReadsTeamsAndPublishes(t *testing.T) {
	f := newFixture(t, "completed")
	ctx := context.Background()
	groups := operatorv1.NewGroupsServiceClient(f.authed)
	opsTeam(t, f)

	list, err := groups.ListGroups(ctx, &operatorv1.ListGroupsRequest{})
	if err != nil || len(list.GetGroups()) != 1 || list.GetGroups()[0].GetName() != "ops" || len(list.GetGroups()[0].GetMembers()) != 2 {
		t.Fatalf("groups: %v %v", list, err)
	}
	group, err := groups.GetGroup(ctx, &operatorv1.GetGroupRequest{Name: "ops", Tenant: "ops"})
	if err != nil || group.GetGroup().GetTenantId() != "ops" || group.GetGroup().GetMembers()[0].GetBotId() != "watcher" || group.GetGroup().GetMembers()[0].GetPlacement() == nil {
		t.Fatalf("group: %v %v", group, err)
	}
	_, err = groups.GetGroup(ctx, &operatorv1.GetGroupRequest{Name: "platform"})
	if k, c := kind(t, err); k != "not_found" || c != "NotFound" {
		t.Errorf("missing group: %s %s", k, c)
	}

	data, _ := structpb.NewStruct(map[string]any{"why": "a storm"})
	stop, err := groups.PublishGroupEvent(ctx, &operatorv1.PublishGroupEventRequest{Name: "ops", Tenant: "ops", EventName: "stop.all", Message: "Everyone stop", IdempotencyKey: "p-1", Data: data})
	if err != nil || !stop.GetCreated() || len(stop.GetDeliveries()) != 2 || stop.GetEventId() == "" {
		t.Fatalf("publish: %v %v", stop, err)
	}
	again, err := groups.PublishGroupEvent(ctx, &operatorv1.PublishGroupEventRequest{Name: "ops", EventName: "stop.all", Message: "Everyone stop", IdempotencyKey: "p-1", Data: data})
	if err != nil || again.GetCreated() || again.GetEventId() != stop.GetEventId() {
		t.Errorf("again: %v %v", again, err)
	}
	_, err = groups.PublishGroupEvent(ctx, &operatorv1.PublishGroupEventRequest{Name: "ops", EventName: "error.bot", Message: "x", IdempotencyKey: "p-2"})
	if k, c := kind(t, err); k != "validation" || c != "InvalidArgument" {
		t.Errorf("error.bot: %s %s", k, c)
	}
	narrowed, err := groups.PublishGroupEvent(ctx, &operatorv1.PublishGroupEventRequest{Name: "ops", EventName: "stop.all", Message: "Only one", IdempotencyKey: "p-3", To: []string{"worker-1"}})
	if err != nil || len(narrowed.GetDeliveries()) != 1 {
		t.Errorf("narrowed: %v %v", narrowed, err)
	}

	topics, err := groups.ListGroupTopics(ctx, &operatorv1.ListGroupTopicsRequest{Name: "ops", Tenant: "ops"})
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, topic := range topics.GetTopics() {
		names = append(names, topic.GetName())
	}
	if !strings.Contains(strings.Join(names, ","), "stop.all") {
		t.Errorf("topics: %v", names)
	}

	events, err := groups.ListGroupEvents(ctx, &operatorv1.ListGroupEventsRequest{Name: "ops", Tenant: "ops"})
	if err != nil || len(events.GetEvents()) == 0 {
		t.Fatalf("timeline: %v %v", events, err)
	}
	var published *operatorv1.Event
	for _, e := range events.GetEvents() {
		if e.GetPayload() != nil && e.GetPayload().GetFields()["eventId"].GetStringValue() == stop.GetEventId() {
			published = e
		}
	}
	if published == nil || published.GetGroupName() != "ops" || published.GetCreatedAt() == nil {
		t.Errorf("the published event is not on the timeline: %v", events.GetEvents())
	}
	if _, err := groups.ListGroupEvents(ctx, &operatorv1.ListGroupEventsRequest{Name: "ops", Limit: -1}); err == nil {
		t.Error("a negative limit was taken")
	}
}

func TestGroupsServiceWatchesTheTimeline(t *testing.T) {
	f := newFixture(t, "completed")
	groups := operatorv1.NewGroupsServiceClient(f.authed)
	opsTeam(t, f)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	existing, _ := groups.ListGroupEvents(ctx, &operatorv1.ListGroupEventsRequest{Name: "ops", Tenant: "ops"})
	cursor := ""
	if n := len(existing.GetEvents()); n > 0 {
		cursor = existing.GetEvents()[n-1].GetEventId()
	}
	stream, err := groups.WatchGroupEvents(ctx, &operatorv1.WatchGroupEventsRequest{Name: "ops", Tenant: "ops", After: cursor})
	if err != nil {
		t.Fatal(err)
	}
	received := make(chan *operatorv1.Event, 4)
	done := make(chan error, 1)
	go func() {
		for {
			message, err := stream.Recv()
			if err != nil {
				done <- err
				return
			}
			received <- message.GetEvent()
		}
	}()
	time.Sleep(50 * time.Millisecond)
	if _, err := groups.PublishGroupEvent(context.Background(), &operatorv1.PublishGroupEventRequest{Name: "ops", EventName: "stop.all", Message: "stop", IdempotencyKey: "w-1"}); err != nil {
		t.Fatal(err)
	}
	select {
	case event := <-received:
		if event.GetEventId() == "" || event.GetType() == "" {
			t.Errorf("event: %v", event)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the watch did not hear the new event")
	}
	cancel()
	if err := <-done; err == io.EOF || err == nil {
		t.Errorf("a cancelled watch ended with %v", err)
	}
}

func TestGroupsServiceWatchOfAnUnknownGroupFails(t *testing.T) {
	f := newFixture(t, "completed")
	stream, err := operatorv1.NewGroupsServiceClient(f.authed).WatchGroupEvents(context.Background(), &operatorv1.WatchGroupEventsRequest{Name: "nobody"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = stream.Recv()
	if k, c := kind(t, err); k != "not_found" || c != "NotFound" {
		t.Errorf("watch: %s %s", k, c)
	}
}
