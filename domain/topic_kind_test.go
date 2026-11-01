package domain

import "testing"

func TestEveryKindFixesItsSeverityAndItsPublishers(t *testing.T) {
	cases := []struct {
		name     EventName
		kind     TopicKind
		severity Severity
		bot      bool
		operator bool
		platform bool
	}{
		{"error.bot", KindError, SeverityCritical, false, false, true},
		{"danger.fire", KindDanger, SeverityCritical, true, true, false},
		{"stop.recall", KindStop, SeverityCritical, true, true, false},
		{"stop", KindStop, SeverityCritical, true, true, false},
		{"warn.stock", KindWarn, SeverityWarning, true, true, true},
		{"done.task", KindDone, SeverityInfo, false, false, true},
		{"info.turno", KindInfo, SeverityInfo, true, true, false},
		{"pedido.nuevo", KindInfo, SeverityInfo, true, true, false},
		{"errors.bot", KindInfo, SeverityInfo, true, true, false},
		{"recall.home", KindInfo, SeverityInfo, true, true, false},
	}
	for _, c := range cases {
		kind := KindOf(c.name)
		if kind != c.kind || kind.Severity() != c.severity || kind.String() != string(c.kind) {
			t.Errorf("%s: kind %s severity %s", c.name, kind, kind.Severity())
		}
		if kind.PublishedBy(PublisherBot) != c.bot || kind.PublishedBy(PublisherOperator) != c.operator || kind.PublishedBy(PublisherControlPlane) != c.platform {
			t.Errorf("%s: publishers bot %v operator %v control plane %v", c.name, kind.PublishedBy(PublisherBot), kind.PublishedBy(PublisherOperator), kind.PublishedBy(PublisherControlPlane))
		}
	}
}
