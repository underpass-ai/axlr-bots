package application

// ModelPolicyDefinition is the model section of a bot definition.
type ModelPolicyDefinition struct {
	Model                string `json:"model,omitempty"`
	RoutingPolicy        string `json:"routingPolicy,omitempty"`
	AllowRemoteProviders bool   `json:"allowRemoteProviders"`
	MaxContextTokens     int    `json:"maxContextTokens,omitempty"`
}
