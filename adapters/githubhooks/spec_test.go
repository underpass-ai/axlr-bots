package githubhooks

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"testing"

	"github.com/underpass-ai/axlr-bots/domain"
	"github.com/underpass-ai/axlr-bots/internal/asyncapigen"
)

const specPath = "../../docs/reference/github-hooks.asyncapi.json"

// The DTOs are the spec's: what go generate writes now is what is in the
// package, and no generated file is left that the spec no longer makes.
func TestTheDTOsAreGeneratedFromTheSpec(t *testing.T) {
	spec, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatal(err)
	}
	files, err := asyncapigen.Generate(spec, filepath.Base(specPath), "githubhooks")
	if err != nil {
		t.Fatal(err)
	}
	for name, want := range files {
		got, err := os.ReadFile(name)
		if err != nil || !bytes.Equal(got, want) {
			t.Errorf("%s is not what the spec generates: run go generate ./adapters/githubhooks", name)
		}
	}
	present, _ := filepath.Glob("*_gen.go")
	for _, name := range present {
		if _, ok := files[name]; !ok {
			t.Errorf("%s is generated from no schema of the spec: remove it", name)
		}
	}
	if len(present) != len(files) || len(files) != 12 {
		t.Errorf("generated %d files, %d in the package", len(files), len(present))
	}
}

// The names the code uses by hand are the spec's: the headers, the
// events GitHub names, the team events published and the channel.
func TestTheNamesAreTheSpecs(t *testing.T) {
	raw, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatal(err)
	}
	var spec struct {
		Channels map[string]struct {
			Address  string `json:"address"`
			Messages map[string]struct {
				Ref string `json:"$ref"`
			} `json:"messages"`
		} `json:"channels"`
		Components struct {
			Messages map[string]struct {
				Name string `json:"name"`
			} `json:"messages"`
			Schemas map[string]struct {
				Required   []string `json:"required"`
				Properties map[string]struct {
					Const    string   `json:"const"`
					Pattern  string   `json:"pattern"`
					Examples []string `json:"examples"`
				} `json:"properties"`
			} `json:"schemas"`
		} `json:"components"`
	}
	if err := json.Unmarshal(raw, &spec); err != nil {
		t.Fatal(err)
	}
	if got := spec.Channels["githubWebhook"].Address; got != Path {
		t.Errorf("channel address %q, the handler serves %q", got, Path)
	}
	headers := spec.Components.Schemas["WebhookHeaders"]
	want := []string{HeaderEvent, HeaderDelivery, HeaderSignature}
	if !slices.Equal(headers.Required, want) || len(headers.Properties) != len(want) {
		t.Errorf("headers %v, the code reads %v", headers.Required, want)
	}
	messages := spec.Components.Messages
	if messages["checkSuite"].Name != GitHubCheckSuite || messages["pullRequest"].Name != GitHubPullRequest || messages["ping"].Name != GitHubPing {
		t.Errorf("GitHub's events = %+v", messages)
	}
	if _, ok := messages["prCi"]; !ok {
		t.Errorf("team events = %+v", messages)
	}
	if _, ok := messages["prMerged"]; !ok {
		t.Errorf("team events = %+v", messages)
	}
	// The events' names are the operator's (--ci-event, --merged-event):
	// the spec fixes none, and its examples are names the platform takes.
	for _, schema := range []string{"PRCIEvent", "PRMergedEvent"} {
		name := spec.Components.Schemas[schema].Properties["name"]
		if name.Const != "" || name.Pattern == "" || len(name.Examples) != 1 {
			t.Errorf("%s fixes its name: %+v", schema, name)
			continue
		}
		if _, err := domain.NewEventName(name.Examples[0]); err != nil || !regexp.MustCompile(name.Pattern).MatchString(name.Examples[0]) {
			t.Errorf("%s: %q is no event name: %v", schema, name.Examples[0], err)
		}
	}
	if len(spec.Channels["githubWebhook"].Messages) != 3 || len(spec.Channels["teamEvents"].Messages) != 2 {
		t.Errorf("channels = %+v", spec.Channels)
	}
}
