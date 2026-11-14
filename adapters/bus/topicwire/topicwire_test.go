package topicwire

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/underpass-ai/axlr-bots/domain"
	"github.com/underpass-ai/axlr-bots/internal/asyncapigen"
)

const specPath = "../../../docs/reference/topics.asyncapi.json"

var at = time.Date(2026, 11, 14, 1, 2, 3, 4, time.FixedZone("x", 3600))

// The DTOs are the spec's: what go generate writes now is what is in
// the package.
func TestTheDTOsAreGeneratedFromTheSpec(t *testing.T) {
	spec, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatal(err)
	}
	files, err := asyncapigen.Generate(spec, filepath.Base(specPath), "topicwire")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 {
		t.Errorf("generated %d files", len(files))
	}
	for name, want := range files {
		got, err := os.ReadFile(name)
		if err != nil || !bytes.Equal(got, want) {
			t.Errorf("%s is not what the spec generates: run go generate ./adapters/bus/topicwire", name)
		}
	}
}

func emitted(by string, teams any) domain.Event {
	payload := map[string]any{
		"eventId": "evt-content", "name": "order.placed", "from": "bot-a", "severity": "info", "message": "order o-1 placed",
		"data": map[string]any{"order": "o-1"}, "to": []string{"bot-b"}, "teams": teams,
	}
	if by != "" {
		payload["by"] = by
	}
	return domain.Event{ID: "evt-1", Type: domain.EventEventEmitted, TenantID: "default", BotID: "bot-a", CorrelationID: "trace-9", CreatedAt: at, Payload: payload}
}

// A bot's event is one envelope per team, with its author; the control
// plane's own about the bot names the bot and the control plane.
func TestABotsEventIsOneEnvelopePerTeam(t *testing.T) {
	got := TopicsOf(emitted("", []string{"team-a", "team.b"}))
	want := TopicEventDTO{
		SpecVersion: "1", ID: "evt-1", Tenant: "default", Team: "team-a", Name: "order.placed", Severity: "info", Message: "order o-1 placed",
		Data: map[string]any{"order": "o-1"}, Publisher: "bot", Bot: "bot-a", To: []string{"bot-b"}, TraceID: "trace-9", At: "2026-11-14T00:02:03.000000004Z",
	}
	if len(got) != 2 || !reflect.DeepEqual(got[0], want) || got[1].Team != "team.b" {
		t.Fatalf("got %+v", got)
	}
	messages, err := EncodeTopics(emitted("", []any{"team-a", "team.b"}))
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 2 || messages[0].Subject != "axlr.topic.default.team-a.order.placed" || messages[1].Subject != "axlr.topic.default.team_b.order.placed" ||
		messages[0].MsgID != "evt-1@team-a" || messages[1].MsgID != "evt-1@team.b" {
		t.Errorf("messages = %+v", messages)
	}
	back, err := DecodeTopic(messages[0].Payload)
	if err != nil || !reflect.DeepEqual(back, want) {
		t.Errorf("back = %+v %v", back, err)
	}

	cp := TopicsOf(emitted("control-plane", []string{"team-a"}))
	if len(cp) != 1 || cp[0].Publisher != "control-plane" || cp[0].Bot != "bot-a" {
		t.Errorf("control plane's = %+v", cp)
	}
	noFrom := emitted("", []string{"team-a"})
	delete(noFrom.Payload, "from")
	if got := TopicsOf(noFrom); got[0].Bot != "bot-a" {
		t.Errorf("without from = %+v", got)
	}
}

// The operator's publication names the operator and no bot; an event of
// another kind is no team's topic.
func TestTheOperatorsPublicationAndOtherEvents(t *testing.T) {
	published := domain.Event{ID: "evt-2", Type: domain.EventGroupEventPublished, TenantID: "default", GroupName: "team-a", CreatedAt: at,
		Payload: map[string]any{"name": "order.placed", "severity": "info", "message": "m", "teams": []string{"team-a"}, "to": []string{}, "data": nil}}
	got := TopicsOf(published)
	if len(got) != 1 || got[0].Publisher != "operator" || got[0].Bot != "" || got[0].Team != "team-a" || got[0].To != nil || got[0].Data != nil || got[0].TraceID != "" {
		t.Fatalf("got %+v", got)
	}
	raw, _ := json.Marshal(got[0])
	for _, absent := range []string{`"bot"`, `"to"`, `"data"`, `"traceId"`} {
		if strings.Contains(string(raw), absent) {
			t.Errorf("%s written: %s", absent, raw)
		}
	}
	for _, other := range []domain.Event{
		{ID: "evt-3", Type: domain.EventTaskCompleted, Payload: map[string]any{"name": "x"}},
		{ID: "evt-4", Type: domain.EventEventEmitted, Payload: map[string]any{"teams": []string{"team-a"}}},
		{ID: "evt-5", Type: domain.EventEventEmitted, Payload: map[string]any{"name": "x", "teams": "team-a"}},
	} {
		if got := TopicsOf(other); len(got) != 0 {
			t.Errorf("%s: %+v", other.ID, got)
		}
		if messages, err := EncodeTopics(other); err != nil || len(messages) != 0 {
			t.Errorf("%s: %+v %v", other.ID, messages, err)
		}
	}
	bad := emitted("", []string{"team-a"})
	bad.Payload["data"] = map[string]any{"f": func() {}}
	if _, err := EncodeTopics(bad); err == nil {
		t.Error("an event that is no JSON encoded")
	}
}

func TestDecodeTopicRefusesWhatIsNoEnvelope(t *testing.T) {
	for _, bad := range []string{
		`nope`,
		`{"specVersion":"2","id":"e","tenant":"t","team":"a","name":"n","severity":"info","message":"m","publisher":"bot","at":"x"}`,
		`{"specVersion":"1","tenant":"t","team":"a","name":"n","severity":"info","message":"m","publisher":"bot","at":"x"}`,
		`{"specVersion":"1","id":"e","tenant":"t","team":"a","name":"n","severity":"info","message":"m","publisher":"someone","at":"x"}`,
		`{"specVersion":"1","id":"e","tenant":"t","team":"a","name":"n","severity":"info","message":"m","publisher":"bot","at":"x","extra":1}`,
		`{"specVersion":"1","id":"e","tenant":"t","team":"a","name":"n","severity":"info","message":"m","publisher":"bot","at":"x"} {}`,
	} {
		if _, err := DecodeTopic([]byte(bad)); err == nil {
			t.Errorf("%s decoded", bad)
		}
	}
}

func TestSubjects(t *testing.T) {
	if got := Token("a.b*c>d e"); got != "a_b_c_d_e" {
		t.Errorf("token = %s", got)
	}
	if got := TeamTopics("default", "team.a"); got != "axlr.topic.default.team_a.>" {
		t.Errorf("team topics = %s", got)
	}
	if got := PublishSubject("default", "team-a", "order.placed"); got != "axlr.publish.default.team-a.order.placed" {
		t.Errorf("publish subject = %s", got)
	}
	address, err := ParsePublishSubject("axlr.publish.default.team_a.order.placed")
	if err != nil || address != (Address{TenantToken: "default", TeamToken: "team_a", Name: "order.placed"}) {
		t.Errorf("address = %+v %v", address, err)
	}
	for _, bad := range []string{"axlr.topic.default.team-a.x", "axlr.publish.default.team-a", "axlr.publish..team-a.x", "axlr.publish.default.team-a."} {
		if _, err := ParsePublishSubject(bad); err == nil {
			t.Errorf("%s parsed", bad)
		}
	}
}

// A publication goes to the wire as an application writes it and comes
// back as the control plane reads it, the operator's command on the
// team; what is no publication of the spec is refused.
func TestPublicationToTheWireAndBack(t *testing.T) {
	p := PublicationDTO{Name: "order.placed", Message: "order o-1 placed", Data: map[string]any{"order": "o-1"}, To: []string{"bot-b"}}
	msg, err := EncodePublication("default", "team-a", "k-1", p)
	if err != nil || msg.Subject != "axlr.publish.default.team-a.order.placed" || msg.MsgID != "k-1" {
		t.Fatalf("message = %+v %v", msg, err)
	}
	read, err := DecodePublication(msg.Subject, msg.Payload, " k-1 ")
	if err != nil || read.Key != "k-1" || read.Address.TeamToken != "team-a" || !reflect.DeepEqual(read.DTO, p) {
		t.Fatalf("read = %+v %v", read, err)
	}
	command := PublicationCommand(read.DTO, "team-a", "default", read.Key)
	if command.Group != "team-a" || command.Tenant != "default" || command.Name != "order.placed" || command.Message != p.Message || command.IdempotencyKey != "k-1" ||
		command.Data["order"] != "o-1" || command.To[0] != "bot-b" {
		t.Errorf("command = %+v", command)
	}
	if _, err := EncodePublication("default", "team-a", "", p); err == nil {
		t.Error("a publication without a key encoded")
	}
	if _, err := EncodePublication("default", "team-a", "k", PublicationDTO{Name: "n", Data: map[string]any{"f": func() {}}}); err == nil {
		t.Error("a publication that is no JSON encoded")
	}
	long := strings.Repeat("k", MaxKeyLength+1)
	for name, c := range map[string]struct{ subject, payload, key string }{
		"subject":    {"axlr.topic.default.team-a.order.placed", string(msg.Payload), "k"},
		"no key":     {msg.Subject, string(msg.Payload), "  "},
		"long key":   {msg.Subject, string(msg.Payload), long},
		"bad json":   {msg.Subject, `{`, "k"},
		"unknown":    {msg.Subject, `{"name":"order.placed","message":"m","team":"team-b"}`, "k"},
		"other name": {msg.Subject, `{"name":"order.shipped","message":"m"}`, "k"},
	} {
		if _, err := DecodePublication(c.subject, []byte(c.payload), c.key); err == nil {
			t.Errorf("%s: decoded", name)
		}
	}
}
