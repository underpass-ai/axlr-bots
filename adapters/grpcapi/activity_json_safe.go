package grpcapi

import "encoding/json"

// jsonSafe turns a payload whose values may be Go structs (an event just
// recorded in memory) into plain JSON values, which a protobuf struct
// takes.
func jsonSafe(payload map[string]any) map[string]any {
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil
	}
	var out map[string]any
	if json.Unmarshal(raw, &out) != nil {
		return nil
	}
	return out
}
