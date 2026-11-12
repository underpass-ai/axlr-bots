package activityaudit

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/underpass-ai/axlr-bots/internal/asyncapigen"
)

const specPath = "../../docs/reference/activity-audit.asyncapi.json"

// The DTOs are the spec's: what go generate writes now is what is in the
// package, and no generated file is left that the spec no longer makes.
func TestTheDTOsAreGeneratedFromTheSpec(t *testing.T) {
	spec, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatal(err)
	}
	files, err := asyncapigen.Generate(spec, filepath.Base(specPath), "activityaudit")
	if err != nil {
		t.Fatal(err)
	}
	for name, want := range files {
		got, err := os.ReadFile(name)
		if err != nil || !bytes.Equal(got, want) {
			t.Errorf("%s is not what the spec generates: run go generate ./adapters/activityaudit", name)
		}
	}
	present, _ := filepath.Glob("*_gen.go")
	for _, name := range present {
		if _, ok := files[name]; !ok {
			t.Errorf("%s is generated from no schema of the spec: remove it", name)
		}
	}
	if len(present) != len(files) || len(files) != 5 {
		t.Errorf("generated %d files, %d in the package", len(files), len(present))
	}
}

// The names the code uses by hand are the spec's: the event, the kinds,
// the channel, and the required fields of the event's data.
func TestTheNamesAreTheSpecs(t *testing.T) {
	raw, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatal(err)
	}
	var spec struct {
		Channels map[string]struct {
			Address  string         `json:"address"`
			Messages map[string]any `json:"messages"`
		} `json:"channels"`
		Components struct {
			Messages map[string]struct {
				Name string `json:"name"`
			} `json:"messages"`
			Schemas map[string]struct {
				Required   []string `json:"required"`
				Properties map[string]struct {
					Const string   `json:"const"`
					Enum  []string `json:"enum"`
				} `json:"properties"`
			} `json:"schemas"`
		} `json:"components"`
	}
	if err := json.Unmarshal(raw, &spec); err != nil {
		t.Fatal(err)
	}
	if spec.Components.Messages["actividadFallo"].Name != EventName || spec.Components.Schemas["ActividadFalloEvent"].Properties["name"].Const != EventName {
		t.Errorf("the spec names another event: %+v", spec.Components.Messages)
	}
	if kinds := spec.Components.Schemas["FalloData"].Properties["kind"].Enum; !slices.Equal(kinds, []string{KindToolCall, KindTaskFailed}) {
		t.Errorf("kinds = %v", kinds)
	}
	if got := spec.Channels["teamEvents"].Address; got != "/v1/groups/{team}/events" || len(spec.Channels) != 1 || len(spec.Channels["teamEvents"].Messages) != 1 {
		t.Errorf("channels = %+v", spec.Channels)
	}
	data := dataOf(FalloData{})
	for _, key := range spec.Components.Schemas["FalloData"].Required {
		if _, ok := data[key]; !ok {
			t.Errorf("the data always carries %s", key)
		}
	}
}
