package application

import (
	"context"
	"slices"
	"time"

	"github.com/underpass-ai/axlr-bots/domain"
)

// ListGroupTopics derives a team's topics: the subscriptions of its
// members (the listeners), the emit rules of its members (the declared
// publishers) and the names seen in the newest 1000 events of its
// timeline (the publishers seen, when and how often).
type ListGroupTopics struct {
	deps Dependencies
}

// NewListGroupTopics wires the use case.
func NewListGroupTopics(deps Dependencies) ListGroupTopics { return ListGroupTopics{deps: deps} }

// seenTopic is what the timeline says of a name.
type seenTopic struct {
	publishers []string
	last       time.Time
	count      int
	addressed  bool
}

// Execute answers the team's topics, by name.
func (uc ListGroupTopics) Execute(ctx context.Context, name, tenant string) ([]GroupTopic, error) {
	group, err := NewGetGroup(uc.deps).Execute(ctx, name, tenant)
	if err != nil {
		return nil, err
	}
	members := make([]domain.Bot, 0, len(group.Members))
	for _, member := range group.Members {
		bot, err := uc.deps.Bots.Get(ctx, domain.BotID(member.BotID))
		if err != nil {
			return nil, err
		}
		members = append(members, bot)
	}
	events, err := NewListGroupEvents(uc.deps).Execute(ctx, GroupEventsQuery{Name: group.Name, Tenant: group.TenantID, Limit: MaxGroupEvents})
	if err != nil {
		return nil, err
	}
	seen := map[string]*seenTopic{}
	for _, event := range events {
		if event.Type != domain.EventEventEmitted && event.Type != domain.EventGroupEventPublished {
			continue
		}
		topic, _ := event.Payload["name"].(string)
		if topic == "" {
			continue
		}
		publisher, _ := event.Payload["from"].(string)
		if by, _ := event.Payload["by"].(string); by == string(domain.PublisherControlPlane) {
			publisher = domain.FromControlPlane
		}
		s := seen[topic]
		if s == nil {
			s = &seenTopic{}
			seen[topic] = s
		}
		if publisher != "" && !slices.Contains(s.publishers, publisher) {
			s.publishers = append(s.publishers, publisher)
		}
		s.count++
		s.last = event.CreatedAt
		s.addressed = addressedPayload(event.Payload["to"])
	}
	names := map[string]bool{}
	for topic := range seen {
		names[topic] = true
	}
	for _, member := range members {
		for _, sub := range member.Subscriptions().List() {
			names[sub.Topic().String()] = true
		}
		for _, rule := range member.Events().Rules() {
			if rule.Pattern() != domain.AnyEvent {
				names[rule.Pattern().String()] = true
			}
		}
	}
	topics := make([]GroupTopic, 0, len(names))
	for topic := range names {
		topics = append(topics, topicOf(topic, seen[topic], members))
	}
	slices.SortFunc(topics, func(a, b GroupTopic) int {
		switch {
		case a.Name < b.Name:
			return -1
		case a.Name > b.Name:
			return 1
		}
		return 0
	})
	return topics, nil
}

// topicOf describes one topic: a name seen or a pattern named.
func topicOf(topic string, seen *seenTopic, members []domain.Bot) GroupTopic {
	out := GroupTopic{Name: topic, Publishers: []string{}, Listeners: []TopicListener{}}
	name, concrete := domain.EventName(topic), seen != nil
	active := 0
	for _, member := range members {
		for _, sub := range member.Subscriptions().List() {
			fits := sub.Topic().String() == topic || concrete && sub.Topic().Matches(name)
			if !fits {
				continue
			}
			listener := TopicListener{BotID: member.ID().String(), Topic: sub.Topic().String(), Mode: sub.Mode().String()}
			if concrete {
				listener.Mode = sub.ModeFor(domain.KindOf(name).Severity()).String()
			}
			if !sub.Active() {
				listener.Status = sub.Status().String()
			} else {
				active++
			}
			out.Listeners = append(out.Listeners, listener)
		}
		for _, rule := range member.Events().Rules() {
			if rule.Pattern() == domain.AnyEvent {
				continue
			}
			if rule.Pattern().String() == topic || concrete && rule.Pattern().Matches(name) {
				out.Declared = append(out.Declared, member.ID().String())
				break
			}
		}
	}
	if concrete {
		out.Publishers = append(out.Publishers, seen.publishers...)
		last := seen.last
		out.LastAt, out.Count = &last, seen.count
		kind := domain.KindOf(name)
		// Every member hears danger and stop; an addressed event is heard
		// by whom it names.
		out.NobodyListens = active == 0 && !seen.addressed && kind != domain.KindDanger && kind != domain.KindStop
	}
	return out
}

// addressedPayload reports whether an event's to names anyone, read
// from memory or from the log.
func addressedPayload(to any) bool {
	switch list := to.(type) {
	case []string:
		return len(list) > 0
	case []any:
		return len(list) > 0
	}
	return false
}
