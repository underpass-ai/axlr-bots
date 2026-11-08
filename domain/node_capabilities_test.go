package domain

import (
	"encoding/json"
	"testing"
)

// A node names its providers by name only; one older than the field names
// none, and its registration still reads.
func TestNodeCapabilitiesNameTheProviders(t *testing.T) {
	var old NodeCapabilities
	if err := json.Unmarshal([]byte(`{"axlrVersion":"0.14.0","policySupported":true,"correlationSupported":true,"runtimes":["jev"]}`), &old); err != nil {
		t.Fatal(err)
	}
	if old.Providers != nil || old.HasProvider("openrouter") {
		t.Fatalf("an old node names no provider: %v", old.Providers)
	}
	if !old.Runs(RuntimeJev) {
		t.Fatal("the runtimes still read")
	}
	out, _ := json.Marshal(old)
	if string(out) != `{"axlrVersion":"0.14.0","policySupported":true,"correlationSupported":true,"runtimes":["jev"]}` {
		t.Fatalf("no providers field for a node that named none: %s", out)
	}
	caps := NodeCapabilities{Providers: []string{"openrouter", "codex", "local:codex/gpt-6.1-sol"}}
	for _, name := range caps.Providers {
		if !caps.HasProvider(name) {
			t.Fatalf("%s is missing", name)
		}
	}
	if caps.HasProvider("jev") {
		t.Fatal("jev was not named")
	}
	out, _ = json.Marshal(caps)
	if string(out) != `{"policySupported":false,"correlationSupported":false,"providers":["openrouter","codex","local:codex/gpt-6.1-sol"]}` {
		t.Fatalf("providers on the wire: %s", out)
	}
}
