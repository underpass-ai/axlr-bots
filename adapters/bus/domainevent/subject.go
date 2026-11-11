package domainevent

import (
	"strings"

	"github.com/underpass-ai/axlr-bots/domain"
)

// SubjectPrefix starts every domain event's subject; the stream that
// keeps them is Stream.
// MsgIDHeader carries the event's identity: JetStream drops a repeat.
const (
	SubjectPrefix = "axlr.domain"
	Stream        = "AXLR_DOMAIN"
	MsgIDHeader   = "Nats-Msg-Id"
)

// Subject is where a domain event of the team is published:
// axlr.domain.<tenant>.<team>.<name>, the name's own dots kept as
// tokens, and in the tenant and the team '.', '*', '>' and spaces
// written '_'.
func Subject(tenant domain.TenantID, team domain.TeamName, name domain.EventName) string {
	return SubjectPrefix + "." + token(tenant.String()) + "." + token(team.String()) + "." + name.String()
}

// TeamSubjects is what a listener of the team's events filters on:
// axlr.domain.<tenant>.<team>.>.
func TeamSubjects(tenant domain.TenantID, team domain.TeamName) string {
	return SubjectPrefix + "." + token(tenant.String()) + "." + token(team.String()) + ".>"
}

func token(raw string) string {
	return strings.NewReplacer(".", "_", "*", "_", ">", "_", " ", "_").Replace(raw)
}
