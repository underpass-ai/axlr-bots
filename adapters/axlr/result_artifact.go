package axlr

// resultArtifact is a deliverable as the result will list it (Phase 1):
// the fields are read leniently since the shape is reserved, not final.
type resultArtifact struct {
	Name      string `json:"name"`
	MediaType string `json:"media_type"`
	Reference string `json:"reference"`
}
