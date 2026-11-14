package grpcclient

import (
	"context"
	"errors"
	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/application/taskactivity"
	"google.golang.org/grpc/metadata"
	"time"

	"google.golang.org/protobuf/types/known/durationpb"

	"github.com/underpass-ai/axlr-bots/adapters/unixapi"
	"github.com/underpass-ai/axlr-bots/domain"
)

type durationpbDuration = durationpb.Duration

func durationpbNew(d time.Duration) *durationpb.Duration { return durationpb.New(d) }

func asValidation(err error, target *domain.ValidationError) bool { return errors.As(err, target) }

func unixapiEnroll(ttl string) unixapi.EnrollTokenRequest {
	return unixapi.EnrollTokenRequest{TTL: ttl}
}

var (
	application0 = application.BotDefinition{}
	patch0       = application.BotPatch{}
	query0       = application.GroupEventsQuery{}
	publish0     = application.PublishGroupEventCommand{}
	taskQuery0   = application.TaskQuery{}
	taskEvents0  = application.TaskEventsQuery{}
	options0     = taskactivity.Options{}
)

func incomingAuthorization(ctx context.Context) string {
	md, _ := metadata.FromIncomingContext(ctx)
	if values := md.Get("authorization"); len(values) > 0 {
		return values[0]
	}
	return ""
}
