package domain

import "strings"

// maxActorLength bounds the name a decision is taken under.
const maxActorLength = 128

// ValidateActor checks the name a person decides under, as the token
// holder asserts it (the CLI's --actor, the panel's operator.actor, the
// X-Axlr-Actor header): not empty, at most 128 characters, no spaces
// and no control characters. Names are compared by ActorKey.
func ValidateActor(field, raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", invalid(field, "must not be empty")
	}
	if len(value) > maxActorLength {
		return "", invalid(field, "must be at most 128 characters")
	}
	for _, r := range value {
		if r <= ' ' || r == 0x7f {
			return "", invalid(field, "must not contain spaces or control characters")
		}
	}
	return value, nil
}

// CLIActorPrefix is what the CLI puts before $USER in its default actor.
const CLIActorPrefix = "cli:"

// PolicyActorPrefix starts the names the control plane decides under as
// a policy (policy:autoApprove, policy:team-event): no person may claim
// one.
const PolicyActorPrefix = "policy:"

// TriggerActorPrefix starts the names a trigger acts under: no person
// may claim one.
const TriggerActorPrefix = "trigger:"

// IsPolicyActor reports a name a policy decides under, however it is
// spelt: POLICY:x and cli:policy:x are policy:x.
func IsPolicyActor(actor string) bool {
	return strings.HasPrefix(ActorKey(actor), PolicyActorPrefix)
}

// IsMachineActor reports a name that says it is no person's: a policy's,
// a trigger's, a bot's or a node's.
func IsMachineActor(actor string) bool {
	key := ActorKey(actor)
	return IsPolicyActor(actor) || strings.HasPrefix(key, TriggerActorPrefix) || strings.HasPrefix(key, "bot:") || strings.HasPrefix(key, "node:")
}

// ActorKey is the name two actors are compared by: without the CLI's
// cli: prefix and in lower case, so alice, cli:alice and Alice are one
// person and never two approvals (review of #139, 8 November 2026).
func ActorKey(actor string) string {
	return strings.TrimPrefix(strings.ToLower(strings.TrimSpace(actor)), CLIActorPrefix)
}
