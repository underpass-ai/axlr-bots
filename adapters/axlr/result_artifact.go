package axlr

// resultArtifact is a deliverable as the result lists it (AXLR's
// artifacts contract: axlr_deliver). Builds before it list only the
// name, the media type and the reference, or nothing; the size and the
// hash are what let the control plane check a copy.
type resultArtifact struct {
	Name        string `json:"name"`
	MediaType   string `json:"media_type"`
	Size        int64  `json:"size"`
	SHA256      string `json:"sha256"`
	Reference   string `json:"reference"`
	Description string `json:"description"`
	Source      string `json:"source"`
	SessionID   string `json:"session_id"`
	CallID      string `json:"call_id"`
}
