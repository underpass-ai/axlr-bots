package domain

// ModelPolicySnapshot is a ModelPolicy as plain data.
type ModelPolicySnapshot struct {
	Model                string `json:"model,omitempty"`
	RoutingPolicy        string `json:"routingPolicy"`
	AllowRemoteProviders bool   `json:"allowRemoteProviders"`
	MaxContextTokens     int    `json:"maxContextTokens"`
}
