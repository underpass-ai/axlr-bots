package domain

import "strings"

// ModelPolicy says which model a bot's work runs on and under which limits.
type ModelPolicy struct {
	model                string
	routing              RoutingPolicy
	allowRemoteProviders bool
	maxContextTokens     int
}

// NewModelPolicy validates a model policy. model may be empty, in which
// case the runtime's default applies; a fixed policy needs a model.
func NewModelPolicy(model, routing string, allowRemoteProviders bool, maxContextTokens int) (ModelPolicy, error) {
	policy, err := ParseRoutingPolicy(routing)
	if err != nil {
		return ModelPolicy{}, err
	}
	model = strings.TrimSpace(model)
	if len(model) > 200 {
		return ModelPolicy{}, invalid("model", "must be at most 200 characters")
	}
	for _, r := range model {
		if r <= 0x20 || r == 0x7f {
			return ModelPolicy{}, invalid("model", "must not contain spaces or control characters")
		}
	}
	if policy == RoutingFixed && model == "" {
		return ModelPolicy{}, invalid("model", "is required by a fixed routing policy")
	}
	if maxContextTokens < 0 {
		return ModelPolicy{}, invalid("max context tokens", "must not be negative")
	}
	return ModelPolicy{model: model, routing: policy, allowRemoteProviders: allowRemoteProviders, maxContextTokens: maxContextTokens}, nil
}

// Model is the model identifier the policy names, or empty for the default.
func (p ModelPolicy) Model() string { return p.model }

// Routing is how the router chooses when the model is not fixed.
func (p ModelPolicy) Routing() RoutingPolicy { return p.routing }

// AllowRemoteProviders says whether the work may leave the host.
func (p ModelPolicy) AllowRemoteProviders() bool { return p.allowRemoteProviders }

// MaxContextTokens caps the context per request; 0 means the runtime's default.
func (p ModelPolicy) MaxContextTokens() int { return p.maxContextTokens }
