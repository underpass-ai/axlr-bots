package axlr

// policyTools are the allow and deny lists of a policy.
type policyTools struct {
	Allow []policyRule `json:"allow"`
	Deny  []policyRule `json:"deny,omitempty"`
}
