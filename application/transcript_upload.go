package application

// TranscriptUpload is one transcript a node hands over: the runtime's
// session id, its bytes and their SHA-256 in hex.
type TranscriptUpload struct {
	Session string
	Content []byte
	SHA256  string
}
