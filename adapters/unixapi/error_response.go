package unixapi

// ErrorResponse is how the server reports an error: the kind names the
// domain error so the client can rebuild it, the message is for people.
// A not-found error also carries what was looked for, so the client
// rebuilds the same domain.NotFoundError.
type ErrorResponse struct {
	Kind     string `json:"kind"`
	Message  string `json:"error"`
	Resource string `json:"resource,omitempty"`
	ID       string `json:"id,omitempty"`
}

// Kinds of errors the API names.
const (
	KindNotFound   = "not_found"
	KindValidation = "validation"
	KindState      = "state"
	KindConflict   = "conflict"
	KindForbidden  = "forbidden"
	KindInternal   = "internal"
)
