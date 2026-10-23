package domain

// Artifact is a deliverable an execution handed over: a name, a media
// type and a reference the control plane can fetch. Reserved by the
// runtime contract for Phase 1; empty until then.
type Artifact struct {
	Name      string `json:"name"`
	MediaType string `json:"mediaType,omitempty"`
	Reference string `json:"reference,omitempty"`
}
