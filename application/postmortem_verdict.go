package application

// PostmortemVerdict is what a verify task of the incident said: green,
// red, or none when its answer names no verdict.
type PostmortemVerdict struct {
	Cycle   int    `json:"cycle"`
	TaskID  string `json:"taskId"`
	Verdict string `json:"verdict"`
}
