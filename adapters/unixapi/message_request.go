package unixapi

// MessageRequest is the body of POST /v1/tasks/{id}/messages: what the
// operator writes to a running task.
type MessageRequest struct {
	Text string `json:"text"`
}
