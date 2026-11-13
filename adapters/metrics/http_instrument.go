package metrics

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// HTTPInstrument counts the requests each listener serves, by route
// pattern, method and status code, and times them. The route is the
// pattern the mux matched ("GET /v1/tasks/{id}"), never the path, so
// no task or execution id becomes a label; a request no route matched
// is "unmatched".
type HTTPInstrument struct {
	requests *prometheus.CounterVec
	duration *prometheus.HistogramVec
}

// NewHTTPInstrument registers its metrics on the registry.
func NewHTTPInstrument(registry prometheus.Registerer) *HTTPInstrument {
	labels := []string{"listener", "route", "code"}
	instrument := &HTTPInstrument{
		requests: prometheus.NewCounterVec(prometheus.CounterOpts{Name: "axlr_bots_http_requests_total", Help: "Requests served, by listener, route pattern and status code."}, labels),
		duration: prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "axlr_bots_http_request_duration_seconds", Help: "Time to serve a request, by listener, route pattern and status code; node streams and long polls are left out.", Buckets: prometheus.DefBuckets}, labels),
	}
	registry.MustRegister(instrument.requests, instrument.duration)
	return instrument
}

// Wrap counts and times what next serves on the listener.
func (i *HTTPInstrument) Wrap(listener string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(recorder, r)
		route := r.Pattern
		if route == "" {
			route = "unmatched"
		}
		code := strconv.Itoa(recorder.status)
		i.requests.WithLabelValues(listener, route, code).Inc()
		if !longLived(route) && !r.URL.Query().Has("wait") {
			i.duration.WithLabelValues(listener, route, code).Observe(time.Since(start).Seconds())
		}
	})
}

// longLived is a route whose request lasts as long as the node is
// connected (the stream) or as long as nothing is due (the long poll):
// timing it says nothing about how fast the control plane answers. A
// request with ?wait= (the event long polls) is left out the same way.
func longLived(route string) bool {
	return strings.HasSuffix(route, "/stream") || strings.HasSuffix(route, "/assignments/next")
}
