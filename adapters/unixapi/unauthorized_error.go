package unixapi

// UnauthorizedError is a request on the operator address without the
// bearer token, or with a wrong one: HTTP 401.
type UnauthorizedError struct{}

func (UnauthorizedError) Error() string {
	return "unauthorized: the operator address takes Authorization: Bearer <token>"
}
