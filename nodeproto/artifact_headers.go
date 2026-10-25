package nodeproto

// Headers of an artifact upload, PUT
// /v1/nodes/{id}/executions/{exec}/artifacts/{name}: the body is the
// artifact's bytes, Content-Type its media type and Content-Length its
// size; the control plane keeps the bytes only when they are that size
// and hash to the SHA-256 the header names. The answer is the artifact
// as kept.
const (
	ArtifactSHA256Header      = "X-Axlr-Artifact-Sha256"
	ArtifactReferenceHeader   = "X-Axlr-Artifact-Reference"
	ArtifactDescriptionHeader = "X-Axlr-Artifact-Description"
)
