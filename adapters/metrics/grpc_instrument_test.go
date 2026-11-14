package metrics

import (
	"context"
	"errors"
	"strings"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestGRPCInstrumentCountsByServiceMethodAndCode(t *testing.T) {
	registry := NewRegistry()
	instrument := NewGRPCInstrument(registry)
	info := &grpc.UnaryServerInfo{FullMethod: "/axlrbots.operator.v1.BotsService/GetBot"}
	ok := func(context.Context, any) (any, error) { return nil, nil }
	missing := func(context.Context, any) (any, error) { return nil, status.Error(codes.NotFound, "no") }
	for _, handler := range []grpc.UnaryHandler{ok, ok, missing} {
		if _, err := instrument.Unary(context.Background(), nil, info, handler); handler == nil && err != nil {
			t.Fatal(err)
		}
	}
	watch := &grpc.StreamServerInfo{FullMethod: "/axlrbots.operator.v1.TasksService/WatchTaskEvents", IsServerStream: true}
	if err := instrument.Stream(nil, nil, watch, func(any, grpc.ServerStream) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if err := instrument.Stream(nil, nil, watch, func(any, grpc.ServerStream) error { return errors.New("boom") }); err == nil {
		t.Fatal("the stream's error was lost")
	}
	body := scrape(t, registry)
	for _, want := range []string{
		`axlr_bots_grpc_requests_total{code="OK",method="GetBot",service="axlrbots.operator.v1.BotsService"} 2`,
		`axlr_bots_grpc_requests_total{code="NotFound",method="GetBot",service="axlrbots.operator.v1.BotsService"} 1`,
		`axlr_bots_grpc_requests_total{code="OK",method="WatchTaskEvents",service="axlrbots.operator.v1.TasksService"} 1`,
		`axlr_bots_grpc_requests_total{code="Unknown",method="WatchTaskEvents",service="axlrbots.operator.v1.TasksService"} 1`,
		`axlr_bots_grpc_request_duration_seconds_count{code="OK",method="GetBot",service="axlrbots.operator.v1.BotsService"} 2`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("metrics lack %s:\n%s", want, body)
		}
	}
	if strings.Contains(body, `duration_seconds_count{code="OK",method="WatchTaskEvents"`) {
		t.Error("a stream was timed")
	}
	if service, method := splitMethod("nonsense"); service != "unknown" || method != "nonsense" {
		t.Errorf("split: %s %s", service, method)
	}
}
