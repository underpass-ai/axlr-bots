package domain

// OutputTargetSnapshot is an output target as plain data: what the
// operator gives and what the repository stores, URL and secret
// included. A webhook has a URL and the Secret its deliveries are signed
// with; a slack target an incoming-webhook URL; a command the program (an
// absolute path) and its arguments. Events, Tenants and Severities filter
// what reaches it: no events is DefaultOutputEvents, no tenants or
// severities is all of them. PanelURL, when set, is the base of the
// panel link of the target's messages.
type OutputTargetSnapshot struct {
	Name       string   `json:"name"`
	Type       string   `json:"type"`
	URL        string   `json:"url,omitempty"`
	Secret     string   `json:"secret,omitempty"`
	Command    []string `json:"command,omitempty"`
	Events     []string `json:"events,omitempty"`
	Tenants    []string `json:"tenants,omitempty"`
	Severities []string `json:"severities,omitempty"`
	PanelURL   string   `json:"panelUrl,omitempty"`
}
