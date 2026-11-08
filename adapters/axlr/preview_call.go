package axlr

import "encoding/json"

// previewCall reports a call whose arguments say confirm: false, directly
// or through the bridge's nested arguments: it shows a change and makes
// none. Arguments the runtime left out say nothing.
func previewCall(raw json.RawMessage) bool {
	if len(raw) == 0 {
		return false
	}
	var call struct {
		Confirm   *bool `json:"confirm"`
		Arguments struct {
			Confirm *bool `json:"confirm"`
		} `json:"arguments"`
	}
	if json.Unmarshal(raw, &call) != nil {
		return false
	}
	return (call.Confirm != nil && !*call.Confirm) || (call.Arguments.Confirm != nil && !*call.Arguments.Confirm)
}
