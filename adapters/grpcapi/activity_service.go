package grpcapi

import (
	"context"
	"time"

	operatorv1 "github.com/underpass-ai/axlr-bots/api/gen/axlrbots/operator/v1"
	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
	"google.golang.org/grpc"
)

// ActivityService serves axlrbots.operator.v1.ActivityService: a task's
// whole activity tree, once or followed live
// (docs/architecture/task-activity.md).
type ActivityService struct {
	operatorv1.UnimplementedActivityServiceServer
	get   application.GetTaskActivity
	watch application.WatchTaskActivity
}

// NewActivityService wires the service over the application's ports;
// changes wakes a watch, heartbeat is how long an idle watch waits to say
// nothing changed (zero: the default).
func NewActivityService(deps application.Dependencies, changes application.ChangeSignal, heartbeat time.Duration) *ActivityService {
	return &ActivityService{get: application.NewGetTaskActivity(deps), watch: application.NewWatchTaskActivity(deps, changes, heartbeat)}
}

// Register adds the service to a gRPC server.
func (s *ActivityService) Register(server grpc.ServiceRegistrar) {
	operatorv1.RegisterActivityServiceServer(server, s)
}

// GetTaskActivity answers the tree as it is now.
func (s *ActivityService) GetTaskActivity(ctx context.Context, req *operatorv1.GetTaskActivityRequest) (*operatorv1.GetTaskActivityResponse, error) {
	id, options, err := activityRequest(req.GetTaskId(), req.GetOptions())
	if err != nil {
		return nil, activityError(err)
	}
	tree, err := s.get.Execute(ctx, id, options)
	if err != nil {
		return nil, activityError(err)
	}
	return &operatorv1.GetTaskActivityResponse{Activity: activityTree(tree)}, nil
}

// WatchTaskActivity streams the snapshot and then what changes, until the
// client cancels or the tree has finished.
func (s *ActivityService) WatchTaskActivity(req *operatorv1.WatchTaskActivityRequest, stream grpc.ServerStreamingServer[operatorv1.WatchTaskActivityResponse]) error {
	id, options, err := activityRequest(req.GetTaskId(), req.GetOptions())
	if err != nil {
		return activityError(err)
	}
	err = s.watch.Execute(stream.Context(), id, options, req.GetResumeCursor(), func(u application.ActivityUpdate) error {
		return stream.Send(activityResponse(u))
	})
	return activityError(err)
}

// activityRequest reads the task id and the options both calls take.
func activityRequest(task string, o *operatorv1.ActivityOptions) (domain.TaskID, application.ActivityOptions, error) {
	id, err := domain.NewTaskID(task)
	if err != nil {
		return "", application.ActivityOptions{}, err
	}
	options, err := activityOptions(o)
	return id, options, err
}
