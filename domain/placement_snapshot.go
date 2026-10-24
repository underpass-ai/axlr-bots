package domain

// PlacementSnapshot is a placement as plain data, as a bot definition
// writes it: {"nodes": ["web-1"], "selector": {"zone": "eu"}}.
type PlacementSnapshot struct {
	Nodes    []string          `json:"nodes,omitempty"`
	Selector map[string]string `json:"selector,omitempty"`
}
