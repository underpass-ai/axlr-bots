package topicwire

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/underpass-ai/axlr-bots/domain"
)

// EncodeTopics writes an event's copies for the wire, one per team it
// was told to; none for an event that is no team's topic.
func EncodeTopics(event domain.Event) ([]Message, error) {
	dtos := TopicsOf(event)
	out := make([]Message, 0, len(dtos))
	for _, dto := range dtos {
		payload, err := json.Marshal(dto)
		if err != nil {
			return nil, fmt.Errorf("topics: event %s: %w", dto.ID, err)
		}
		out = append(out, Message{Subject: TopicSubject(dto.Tenant, dto.Team, dto.Name), MsgID: TopicMsgID(dto.ID, dto.Team), Payload: payload})
	}
	return out, nil
}

// DecodeTopic reads a team's event off the wire, as an application does;
// an error is a message that is no envelope of the spec.
func DecodeTopic(payload []byte) (TopicEventDTO, error) {
	var dto TopicEventDTO
	if err := strict(payload, &dto); err != nil {
		return TopicEventDTO{}, err
	}
	switch {
	case dto.SpecVersion != SpecVersion:
		return TopicEventDTO{}, fmt.Errorf("topics: specVersion %q, want %q", dto.SpecVersion, SpecVersion)
	case dto.ID == "" || dto.Tenant == "" || dto.Team == "" || dto.Name == "" || dto.At == "":
		return TopicEventDTO{}, fmt.Errorf("topics: an event needs its id, tenant, team, name and at")
	case dto.Publisher != PublisherBot && dto.Publisher != PublisherOperator && dto.Publisher != PublisherControlPlane:
		return TopicEventDTO{}, fmt.Errorf("topics: publisher %q is none of bot, operator, control-plane", dto.Publisher)
	}
	return dto, nil
}

// EncodePublication writes a publication on a team for the wire, as an
// application does: its subject, its key and its payload.
func EncodePublication(tenant, team, key string, p PublicationDTO) (Message, error) {
	if err := checkKey(key); err != nil {
		return Message{}, err
	}
	payload, err := json.Marshal(p)
	if err != nil {
		return Message{}, fmt.Errorf("topics: publication: %w", err)
	}
	return Message{Subject: PublishSubject(tenant, team, p.Name), MsgID: key, Payload: payload}, nil
}

// DecodePublication reads a publication: its subject, its payload and
// the Nats-Msg-Id it came with. An error is a publication refused for
// good (a poison message: retrying it mends nothing): a subject that is
// no publication's, a missing or overlong key, a payload that is no
// envelope of the spec, or a name that is not the subject's.
func DecodePublication(subject string, payload []byte, key string) (Publication, error) {
	address, err := ParsePublishSubject(subject)
	if err != nil {
		return Publication{}, err
	}
	if err := checkKey(key); err != nil {
		return Publication{}, err
	}
	var dto PublicationDTO
	if err := strict(payload, &dto); err != nil {
		return Publication{}, err
	}
	if dto.Name != address.Name {
		return Publication{}, fmt.Errorf("topics: the publication's name %q is not its subject's, %q", dto.Name, address.Name)
	}
	return Publication{Address: address, Key: strings.TrimSpace(key), DTO: dto}, nil
}

func checkKey(key string) error {
	trimmed := strings.TrimSpace(key)
	if trimmed == "" {
		return fmt.Errorf("topics: a publication needs its idempotency key in %s", MsgIDHeader)
	}
	if len(trimmed) > MaxKeyLength {
		return fmt.Errorf("topics: the idempotency key is longer than %d characters", MaxKeyLength)
	}
	return nil
}

// strict decodes one JSON object into v, refusing fields the spec does
// not describe and anything after it.
func strict(payload []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(payload))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("topics: %w", err)
	}
	if dec.More() {
		return fmt.Errorf("topics: trailing data after the envelope")
	}
	return nil
}
