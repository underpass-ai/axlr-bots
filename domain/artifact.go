package domain

// Artifact is a deliverable an execution handed over: its name, media
// type and the reference it was delivered under (a path relative to the
// workspace the execution ran in, for AXLR's axlr_deliver; the report's
// path for the control plane's own report), with the size and SHA-256
// of its bytes, what it is and the call that delivered it, as AXLR's
// result lists them (absent from builds before the artifacts contract).
// Stored says the control plane keeps a copy of the bytes, checked
// against the size and the hash, served by name under its execution.
type Artifact struct {
	Name        string `json:"name"`
	MediaType   string `json:"mediaType,omitempty"`
	Reference   string `json:"reference,omitempty"`
	Size        int64  `json:"size,omitempty"`
	SHA256      string `json:"sha256,omitempty"`
	Description string `json:"description,omitempty"`
	Source      string `json:"source,omitempty"`
	SessionID   string `json:"sessionId,omitempty"`
	CallID      string `json:"callId,omitempty"`
	Stored      bool   `json:"stored,omitempty"`
}

// ArtifactSourceAnswer marks the report the control plane writes from a
// completed run's answer, beside the artifacts the runtime delivered.
const ArtifactSourceAnswer = "answer"

// Verifiable reports an artifact whose bytes can be checked: it has a
// valid name, a size within the bounds and a SHA-256, as AXLR lists a
// delivery. An artifact without them (an older runtime) is listed and
// not copied.
func (a Artifact) Verifiable() bool {
	return ValidateArtifactName(a.Name) == nil && a.Size >= 0 && a.Size <= MaxArtifactBytes && isSHA256(a.SHA256) && a.Source != ArtifactSourceAnswer
}

func isSHA256(raw string) bool {
	if len(raw) != 64 {
		return false
	}
	for _, r := range raw {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}
