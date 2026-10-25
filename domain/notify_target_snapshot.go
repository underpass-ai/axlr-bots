package domain

// NotifyTargetSnapshot is a notification target as plain data: what a
// definition says and what the repository stores. A webhook has a URL
// and the Secret its deliveries are signed with; a command is the
// program (an absolute path) and its arguments. Name defaults to the
// type.
type NotifyTargetSnapshot struct {
	Type    string   `json:"type"`
	Name    string   `json:"name,omitempty"`
	URL     string   `json:"url,omitempty"`
	Secret  string   `json:"secret,omitempty"`
	Command []string `json:"command,omitempty"`
}

// Redacted is the snapshot with its secret hidden.
func (s NotifyTargetSnapshot) Redacted() NotifyTargetSnapshot {
	if s.Secret != "" {
		s.Secret = Redacted
	}
	s.Command = append([]string(nil), s.Command...)
	return s
}
