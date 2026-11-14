package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunWritesTheFiles(t *testing.T) {
	dir := t.TempDir()
	spec := filepath.Join(dir, "spec.json")
	_ = os.WriteFile(spec, []byte(`{"asyncapi": "3.0.0", "components": {"schemas": {"A": {"type": "object", "x-go-type": "wire.A", "properties": {"x": {"type": "string"}}}}}}`), 0o644)
	var out bytes.Buffer
	if err := run([]string{"-spec", spec, "-package", "wire", "-out", dir}, &out); err != nil {
		t.Fatal(err)
	}
	code, err := os.ReadFile(filepath.Join(dir, "a_gen.go"))
	if err != nil || !strings.Contains(string(code), "type A struct") || !strings.Contains(out.String(), "wrote a_gen.go") {
		t.Errorf("code %s, out %s, %v", code, out.String(), err)
	}
	for name, args := range map[string][]string{
		"no spec":  {"-package", "wire"},
		"missing":  {"-spec", filepath.Join(dir, "none.json"), "-package", "wire"},
		"bad flag": {"-nope"},
		"bad spec": {"-spec", spec, "-package", "other"},
		"bad out":  {"-spec", spec, "-package", "wire", "-out", filepath.Join(dir, "none")},
	} {
		if err := run(args, &out); err == nil {
			t.Errorf("%s: ran", name)
		}
	}
}
