package application

import "github.com/underpass-ai/axlr-bots/domain"

// OutputView is an output target as anyone may read it: its name, type
// and filters, the host it posts to, and a fingerprint that changes with
// its URL, secret or command. Neither the URL (a chat webhook's is its
// secret) nor the secret nor the command is in it.
type OutputView struct {
	Name        string   `json:"name"`
	Type        string   `json:"type"`
	Fingerprint string   `json:"fingerprint"`
	Host        string   `json:"host,omitempty"`
	Events      []string `json:"events"`
	Tenants     []string `json:"tenants,omitempty"`
	Severities  []string `json:"severities,omitempty"`
	PanelURL    string   `json:"panelUrl,omitempty"`
}

// NewOutputView reads a target without its secrets.
func NewOutputView(target domain.OutputTarget) OutputView {
	s := target.Snapshot()
	events := make([]string, 0, len(target.Events()))
	for _, event := range target.Events() {
		events = append(events, event.String())
	}
	return OutputView{
		Name: s.Name, Type: s.Type, Fingerprint: target.Fingerprint(), Host: target.Host(),
		Events: events, Tenants: s.Tenants, Severities: s.Severities, PanelURL: s.PanelURL,
	}
}
