package unixapi

import "net/http"

// Middleware wraps the handler a listener serves; listener names it:
// "socket", "nodes" or "operator".
type Middleware func(listener string, next http.Handler) http.Handler

// WithMiddleware is the server with the middleware around each
// listener's handler.
func (s *Server) WithMiddleware(wrap Middleware) *Server {
	s.wrap = wrap
	return s
}

// wrapped is the handler with the middleware around it, if any.
func (s *Server) wrapped(listener string, handler http.Handler) http.Handler {
	if s.wrap == nil {
		return handler
	}
	return s.wrap(listener, handler)
}
