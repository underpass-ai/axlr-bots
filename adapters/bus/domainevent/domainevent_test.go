package domainevent

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
	"github.com/underpass-ai/axlr-bots/internal/asyncapigen"
)

const specPath = "../../../docs/reference/domain-events.asyncapi.json"

var at = time.Date(2026, 11, 12, 1, 2, 3, 4, time.FixedZone("x", 3600))

func event(t *testing.T, context domain.ContextKey) domain.DomainEvent {
	t.Helper()
	e, err := domain.NewDomainEvent("incidencia.nueva", "h-20261112-kmp-3", context, "qa", "qa-triaje", at)
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func dev(t *testing.T) domain.Audience {
	t.Helper()
	a, err := domain.NewAudience("qa", []domain.TeamName{"dev"})
	if err != nil {
		t.Fatal(err)
	}
	return a
}

// A crossing event goes to the wire and back whole; what is no domain
// event of the spec is refused.
func TestEncodeAndDecode(t *testing.T) {
	in := application.CrossingEvent{Tenant: "default", Event: event(t, "project:qa.hallazgo.h-1"), Audience: dev(t), TraceID: "trace-9", Crossings: 2}
	msg, err := Encode(in)
	if err != nil {
		t.Fatal(err)
	}
	if msg.Subject != "axlr.domain.default.qa.incidencia.nueva" || msg.ID != in.Event.Identity() {
		t.Errorf("message = %+v", msg)
	}
	out, err := Decode(msg.Payload)
	if err != nil {
		t.Fatal(err)
	}
	if out.Tenant != in.Tenant || out.Event != in.Event || out.TraceID != "trace-9" || out.Crossings != 2 || !out.Audience.Has("dev") {
		t.Errorf("out = %+v", out)
	}
	if out.Origin() != in.Origin() || in.Origin().Crossings != 2 || in.Origin().Context != "project:qa.hallazgo.h-1" {
		t.Errorf("origin = %+v", out.Origin())
	}
	for _, bad := range []string{"nope", `{"specVersion":"1"}`} {
		if _, err := Decode([]byte(bad)); err == nil {
			t.Errorf("%s decoded", bad)
		}
	}
}

// The DTOs are the spec's: what go generate writes now is what is in the
// package.
func TestTheDTOsAreGeneratedFromTheSpec(t *testing.T) {
	spec, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatal(err)
	}
	files, err := asyncapigen.Generate(spec, filepath.Base(specPath), "domainevent")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 {
		t.Errorf("generated %d files", len(files))
	}
	for name, want := range files {
		got, err := os.ReadFile(name)
		if err != nil || !bytes.Equal(got, want) {
			t.Errorf("%s is not what the spec generates: run go generate ./adapters/bus/domainevent", name)
		}
	}
}

func TestEventToDTOAndBack(t *testing.T) {
	e := event(t, "project:qa.hallazgo.h-20261112-kmp-3")
	dto := ToDTO(e, "default", "trace-1", 1, dev(t))
	if len(dto.Audience) != 1 || dto.Audience[0] != "dev" {
		t.Errorf("audience = %v", dto.Audience)
	}
	if dto.SpecVersion != SpecVersion || dto.ID != e.Identity() || dto.Name != "incidencia.nueva" || dto.Tenant != "default" || dto.Team != "qa" ||
		dto.From != "qa-triaje" || dto.OccurredAt != "2026-11-12T00:02:03.000000004Z" || dto.TraceID != "trace-1" || dto.Crossings != 1 ||
		dto.Data != (DataDTO{ID: "h-20261112-kmp-3", Context: "project:qa.hallazgo.h-20261112-kmp-3"}) {
		t.Errorf("dto = %+v", dto)
	}
	raw, _ := json.Marshal(dto)
	var read DTO
	if err := json.Unmarshal(raw, &read); err != nil {
		t.Fatal(err)
	}
	back, tenant, origin, audience, err := FromDTO(read)
	if err != nil {
		t.Fatal(err)
	}
	if tenant != "default" || !audience.Has("dev") || len(audience.Teams()) != 1 {
		t.Errorf("tenant %s, audience %v", tenant, audience.Strings())
	}
	if back != e {
		t.Errorf("back = %+v, want %+v", back, e)
	}
	if origin != (domain.EventOrigin{EventID: e.Identity(), From: "qa-triaje", Team: "qa", TraceID: "trace-1", Crossings: 1, Context: "project:qa.hallazgo.h-20261112-kmp-3"}) {
		t.Errorf("origin = %+v", origin)
	}
	plain, _ := json.Marshal(ToDTO(event(t, ""), "default", "", 2, dev(t)))
	if strings.Contains(string(plain), "context") || strings.Contains(string(plain), "traceId") {
		t.Errorf("empty optionals written: %s", plain)
	}
}

func TestFromDTORefusesWhatIsNotTheSpecsEvent(t *testing.T) {
	good := ToDTO(event(t, ""), "default", "trace-1", 1, dev(t))
	cases := map[string]func(*DTO){
		"version":       func(d *DTO) { d.SpecVersion = "2" },
		"name":          func(d *DTO) { d.Name = "Bad Name" },
		"control":       func(d *DTO) { d.Name = "stop.all" },
		"thing id":      func(d *DTO) { d.Data.ID = "" },
		"context":       func(d *DTO) { d.Data.Context = "not a key" },
		"time":          func(d *DTO) { d.OccurredAt = "yesterday" },
		"team":          func(d *DTO) { d.Team = "Q A" },
		"forged id":     func(d *DTO) { d.ID = "evt-000000000000000000000000" },
		"changed thing": func(d *DTO) { d.Data.ID = "h-other" },
		"fifth":         func(d *DTO) { d.Crossings = 5 },
		"no crossing":   func(d *DTO) { d.Crossings = 0 },
		"no tenant":     func(d *DTO) { d.Tenant = "" },
		"no audience":   func(d *DTO) { d.Audience = nil },
		"repeated team": func(d *DTO) { d.Audience = []string{"dev", "dev"} },
		"own team":      func(d *DTO) { d.Audience = []string{"dev", "qa"} },
	}
	for name, change := range cases {
		d := good
		change(&d)
		if _, _, _, _, err := FromDTO(d); err == nil {
			t.Errorf("%s: accepted %+v", name, d)
		}
	}
}

func TestTheSubjects(t *testing.T) {
	if got := Subject("default", "qa", "incidencia.nueva"); got != "axlr.domain.default.qa.incidencia.nueva" {
		t.Errorf("subject = %s", got)
	}
	if got := Subject("a b", "mkt.editorial", "x.y"); got != "axlr.domain.a_b.mkt_editorial.x.y" {
		t.Errorf("written = %s", got)
	}
	if got := TeamSubjects("default", "dev"); got != "axlr.domain.default.dev.>" {
		t.Errorf("team subjects = %s", got)
	}
}

// Every channel of the spec is a subject Subject writes, under the stream's
// prefix, and every message is the generated DTO with its Nats-Msg-Id.
func TestTheSpecsChannelsAreTheSubjects(t *testing.T) {
	raw, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatal(err)
	}
	var spec struct {
		Channels map[string]struct {
			Address    string                     `json:"address"`
			Parameters map[string]json.RawMessage `json:"parameters"`
		} `json:"channels"`
		Components struct {
			Messages map[string]struct {
				Headers struct {
					Required []string `json:"required"`
				} `json:"headers"`
			} `json:"messages"`
		} `json:"components"`
	}
	if err := json.Unmarshal(raw, &spec); err != nil {
		t.Fatal(err)
	}
	if len(spec.Channels) == 0 {
		t.Fatal("no channels")
	}
	for id, ch := range spec.Channels {
		name, ok := strings.CutPrefix(ch.Address, SubjectPrefix+".{tenant}.{team}.")
		if !ok || len(ch.Parameters) != 2 {
			t.Errorf("%s: address %q is not %s.{tenant}.{team}.<name>", id, ch.Address, SubjectPrefix)
			continue
		}
		event, err := domain.NewEventName(name)
		if err != nil || domain.KindOf(event) != domain.KindInfo {
			t.Errorf("%s: %q is no domain event name", id, name)
			continue
		}
		if Subject("default", "qa", event) != SubjectPrefix+".default.qa."+name {
			t.Errorf("%s: Subject writes another address", id)
		}
	}
	if headers := spec.Components.Messages["domainEvent"].Headers.Required; len(headers) != 1 || headers[0] != MsgIDHeader {
		t.Errorf("headers = %v", headers)
	}
}
