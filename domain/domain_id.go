package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strings"
)

// maxDomainIDLength bounds the id a domain event carries in data.id.
const maxDomainIDLength = 128

var domainIDPattern = regexp.MustCompile(`^[A-Za-z0-9._:-]+$`)

// DomainID is the id of the thing a domain event is about (a finding, a
// fix): the data.id of an event that crosses to another team
// (docs/architecture/team-topics.md).
type DomainID string

// NewDomainID validates an id: 1 to 128 letters, digits, '.', '_', ':'
// and '-'.
func NewDomainID(raw string) (DomainID, error) {
	value := strings.TrimSpace(raw)
	if value == "" || len(value) > maxDomainIDLength || !domainIDPattern.MatchString(value) {
		return "", invalid("data.id", "must be 1 to 128 letters, digits, '.', '_', ':' or '-'")
	}
	return DomainID(value), nil
}

// EventIdentity is the id of the domain event: the same fact (team,
// name, id) published twice, by any member of the team, is one event.
func (id DomainID) EventIdentity(team TeamName, name EventName) string {
	sum := sha256.Sum256([]byte(team.String() + "\x00" + name.String() + "\x00" + string(id)))
	return "evt-" + hex.EncodeToString(sum[:12])
}

func (id DomainID) String() string { return string(id) }
