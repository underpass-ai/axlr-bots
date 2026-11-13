package metrics

import "net/http"

// statusRecorder keeps the status code a handler writes. It flushes
// through, for the node stream, and unwraps, for
// http.ResponseController.
type statusRecorder struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (r *statusRecorder) WriteHeader(code int) {
	if !r.wroteHeader {
		r.status = code
		r.wroteHeader = true
	}
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	r.wroteHeader = true
	return r.ResponseWriter.Write(b)
}

// Flush flushes the writer underneath when it can.
func (r *statusRecorder) Flush() {
	if flusher, ok := r.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

// Unwrap is the writer underneath.
func (r *statusRecorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }
