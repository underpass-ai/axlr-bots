package metrics

import (
	"context"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/status"
)

// GRPCInstrument counts the calls the operator API serves over gRPC, by
// service, method and status code, and times the unary ones. A server
// stream (a watch, a download) is counted but not timed: it lasts as
// long as the client stays, and says nothing about how fast the control
// plane answers. The labels are the full method's parts, never an id.
type GRPCInstrument struct {
	requests *prometheus.CounterVec
	duration *prometheus.HistogramVec
}

// NewGRPCInstrument registers its metrics on the registry.
func NewGRPCInstrument(registry prometheus.Registerer) *GRPCInstrument {
	labels := []string{"service", "method", "code"}
	instrument := &GRPCInstrument{
		requests: prometheus.NewCounterVec(prometheus.CounterOpts{Name: "axlr_bots_grpc_requests_total", Help: "gRPC calls served, by service, method and status code."}, labels),
		duration: prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "axlr_bots_grpc_request_duration_seconds", Help: "Time to serve a unary gRPC call, by service, method and status code; streams are left out.", Buckets: prometheus.DefBuckets}, labels),
	}
	registry.MustRegister(instrument.requests, instrument.duration)
	return instrument
}

// Unary counts and times the unary calls.
func (i *GRPCInstrument) Unary(ctx context.Context, request any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
	start := time.Now()
	response, err := handler(ctx, request)
	service, method := splitMethod(info.FullMethod)
	code := status.Code(err).String()
	i.requests.WithLabelValues(service, method, code).Inc()
	i.duration.WithLabelValues(service, method, code).Observe(time.Since(start).Seconds())
	return response, err
}

// Stream counts the streaming calls once they end.
func (i *GRPCInstrument) Stream(srv any, stream grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
	err := handler(srv, stream)
	service, method := splitMethod(info.FullMethod)
	i.requests.WithLabelValues(service, method, status.Code(err).String()).Inc()
	return err
}

// splitMethod is "/pkg.Service/Method" as its service and method.
func splitMethod(full string) (string, string) {
	service, method, ok := strings.Cut(strings.TrimPrefix(full, "/"), "/")
	if !ok {
		return "unknown", full
	}
	return service, method
}
