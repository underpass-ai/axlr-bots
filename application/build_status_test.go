package application

import (
	"errors"
	"strings"
	"testing"

	"github.com/underpass-ai/axlr-bots/domain"
)

type fixedBuilds struct {
	running, installed domain.BuildIdentity
	err                error
}

func (f fixedBuilds) Running() domain.BuildIdentity            { return f.running }
func (f fixedBuilds) Installed() (domain.BuildIdentity, error) { return f.installed, f.err }

func TestCheckBuild(t *testing.T) {
	a, b := domain.BuildIdentity{Revision: "aaa"}, domain.BuildIdentity{Revision: "bbb"}
	cases := []struct {
		name   string
		reader BuildReader
		stale  bool
		reason string
	}{
		{"no reader", nil, false, "no build reader"},
		{"same build", fixedBuilds{running: a, installed: a}, false, ""},
		{"newer binary installed", fixedBuilds{running: a, installed: b}, true, ""},
		{"unreadable binary", fixedBuilds{running: a, err: errors.New("gone")}, false, "cannot be read: gone"},
		{"running without commit", fixedBuilds{installed: b}, false, "running build names no commit"},
		{"installed without commit", fixedBuilds{running: a}, false, "installed binary names no commit"},
	}
	for _, c := range cases {
		status := CheckBuild(c.reader)
		if status.Stale != c.stale || !strings.Contains(status.Reason, c.reason) || (c.reason == "" && status.Reason != "") {
			t.Errorf("%s: %+v", c.name, status)
		}
	}
	if status := CheckBuild(fixedBuilds{running: a, installed: b}); status.Running != a || status.Installed != b {
		t.Errorf("both builds are kept: %+v", status)
	}
}
