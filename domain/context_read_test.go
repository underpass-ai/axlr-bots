package domain

import (
	"strings"
	"testing"
)

func TestAContextRefIsValidated(t *testing.T) {
	if err := (ContextRef{Key: "ws:s1", Team: "qa"}).Validate(); err != nil {
		t.Error(err)
	}
	for _, bad := range []ContextRef{{Key: "no key", Team: "qa"}, {Key: "ws:s1", Team: "Bad Team"}, {Key: "", Team: "qa"}} {
		if bad.Validate() == nil {
			t.Errorf("%+v accepted", bad)
		}
	}
}

func TestAReadIsBoundedAndRendered(t *testing.T) {
	long := strings.Repeat("x", 100)
	var lines []string
	for i := 0; i < 40; i++ {
		lines = append(lines, long)
	}
	read := ContextRead{Key: "ws:s1", Team: "qa", Read: append([]string{"  newest   line  ", ""}, lines...), Line: strings.Repeat("é", 400)}.Bounded()
	total := 0
	for _, line := range read.Read {
		total += len(line) + 1
	}
	if read.Read[0] != "newest line" || total > MaxContextReadBytes || len([]rune(read.Line)) != 300 {
		t.Errorf("bounded = %d lines, %d bytes, first %q", len(read.Read), total, read.Read[0])
	}
	huge := ContextRead{Key: "ws:s1", Team: "qa", Read: []string{strings.Repeat("y", 5000)}}.Bounded()
	if len(huge.Read) != 1 || len(huge.Read[0]) >= MaxContextReadBytes {
		t.Errorf("one huge line = %d bytes", len(huge.Read[0]))
	}
	reads := []ContextRead{{Key: "ws:s1", Team: "qa", Read: []string{"a", "b"}}, {Key: "project:dev.x", Team: "dev", Line: "ask dev"}}
	text := RenderContextReads(reads, 1000)
	if text != EarlierContextHeading+"\nws:s1 (team qa):\n- a\n- b\nproject:dev.x (team dev): ask dev" {
		t.Errorf("text = %q", text)
	}
	if cut := RenderContextReads(reads, 70); len(cut) > 70 || !strings.HasSuffix(cut, "…") {
		t.Errorf("cut = %q", cut)
	}
	if RenderContextReads(reads, 10) != "" || RenderContextReads(nil, 1000) != "" {
		t.Error("no room, or nothing, rendered something")
	}
}

func TestATaskKeepsWhatItsNodeRead(t *testing.T) {
	task := sampleTask(t)
	if err := task.KeepContext(nil); err != nil || task.Earlier() != nil {
		t.Fatal("nothing kept something")
	}
	if err := task.KeepContext([]ContextRead{{Key: "bad key", Team: "qa"}}); err == nil {
		t.Error("a bad read was kept")
	}
	reads := []ContextRead{{Key: "ws:s1", Team: "qa", Read: []string{"a"}}, {Key: "ws:s2", Team: "qa", Line: "x"}, {Key: "ws:s3", Team: "qa", Line: "y"}}
	if err := task.KeepContext(reads); err != nil {
		t.Fatal(err)
	}
	if got := task.Earlier(); len(got) != MaxContextRefs || got[0].Read[0] != "a" {
		t.Errorf("earlier = %+v", got)
	}
	// Kept once: a later report changes nothing.
	_ = task.KeepContext([]ContextRead{{Key: "ws:s9", Team: "qa", Line: "z"}})
	if task.Earlier()[0].Key != "ws:s1" {
		t.Error("kept twice")
	}
	// An input that is no object is kept under input.
	other := sampleTask(t)
	_ = other.Attribute(other.Trigger(), []byte(`[1,2]`))
	if err := other.KeepContext(reads[:1]); err != nil || !strings.Contains(string(other.Input()), `"input":[1,2]`) || len(other.Earlier()) != 1 {
		t.Errorf("input = %s %v", other.Input(), err)
	}
	broken := sampleTask(t)
	_ = broken.Attribute(broken.Trigger(), []byte(`"text"`))
	if broken.Earlier() != nil {
		t.Error("a string input has earlier")
	}
}
