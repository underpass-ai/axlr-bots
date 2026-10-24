package unixapi

// ReportResponse is a task's report: where it is kept and its text.
type ReportResponse struct {
	TaskID    string `json:"taskId"`
	Reference string `json:"reference,omitempty"`
	Content   string `json:"content"`
}
