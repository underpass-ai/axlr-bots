package application

import "github.com/underpass-ai/axlr-bots/domain"

// OutputDefinition is an output target as the operator gives it (POST
// /v1/outputs, axlr-bots output add): a name, a type and what the type
// needs, and the filters. Events take the full name (incident.opened) or
// the short one (opened); none is the default list.
type OutputDefinition struct {
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

// target validates the definition.
func (d OutputDefinition) target() (domain.OutputTarget, error) {
	return domain.NewOutputTarget(domain.OutputTargetSnapshot{
		Name: d.Name, Type: d.Type, URL: d.URL, Secret: d.Secret, Command: d.Command,
		Events: d.Events, Tenants: d.Tenants, Severities: d.Severities, PanelURL: d.PanelURL,
	})
}
