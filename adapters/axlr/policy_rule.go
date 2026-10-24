package axlr

// policyRule names a tool, with optional argument patterns.
type policyRule struct {
	Name      string            `json:"name"`
	Arguments map[string]string `json:"arguments,omitempty"`
}
