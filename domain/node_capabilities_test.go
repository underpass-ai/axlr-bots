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

// A node names the teams whose memory it shares; one older than the field
// names none and is never picked to answer a team's question.
func TestNodeCapabilitiesNameTheMemoryTeams(t *testing.T) {
	var old NodeCapabilities
	if err := json.Unmarshal([]byte(`{"policySupported":true,"correlationSupported":true}`), &old); err != nil {
		t.Fatal(err)
	}
	if old.MemoryTeams != nil || old.SharesMemoryOf("qa") {
		t.Fatalf("an old node shares no team's memory: %v", old.MemoryTeams)
	}
	if out, _ := json.Marshal(old); string(out) != `{"policySupported":true,"correlationSupported":true}` {
		t.Errorf("no memoryTeams field for a node that named none: %s", out)
	}
	named := NodeCapabilities{MemoryTeams: []string{"qa", "dev"}}
	if !named.SharesMemoryOf("dev") || named.SharesMemoryOf("marketing") || named.SharesMemoryOf("") {
		t.Errorf("named = %v", named.MemoryTeams)
	}
	every := NodeCapabilities{MemoryTeams: []string{AllMemoryTeams}}
	if !every.SharesMemoryOf("marketing") || every.SharesMemoryOf("") {
		t.Error("* shares every team's memory, and a bot in no team has none")
	}
	if out, _ := json.Marshal(every); string(out) != `{"policySupported":false,"correlationSupported":false,"memoryTeams":["*"]}` {
		t.Errorf("written: %s", out)
	}
}
