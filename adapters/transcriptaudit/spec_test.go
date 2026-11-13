package transcriptaudit

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/underpass-ai/axlr-bots/internal/asyncapigen"
)

const specPath = "../../docs/reference/transcript-audit.asyncapi.json"

// The DTOs are the spec's.
func TestTheDTOsAreGeneratedFromTheSpec(t *testing.T) {
	spec, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatal(err)
	}
	files, err := asyncapigen.Generate(spec, filepath.Base(specPath), "transcriptaudit")
	if err != nil {
		t.Fatal(err)
	}
	for name, want := range files {
		got, err := os.ReadFile(name)
		if err != nil || !bytes.Equal(got, want) {
			t.Errorf("%s is not what the spec generates: run go generate ./adapters/transcriptaudit", name)
		}
	}
	present, _ := filepath.Glob("*_gen.go")
	for _, name := range present {
		if _, ok := files[name]; !ok {
			t.Errorf("%s is generated from no schema of the spec: remove it", name)
		}
	}
}
