package grpcclient

import (
	"context"
	"errors"
	"io"

	operatorv1 "github.com/underpass-ai/axlr-bots/api/gen/axlrbots/operator/v1"
	"github.com/underpass-ai/axlr-bots/application/taskactivity"
)

var _ taskactivity.Reader = (*Client)(nil)

// Get answers the task's activity tree as it is now. The snapshot is one
// message that can pass 4 MiB: the connection takes it.
func (c *Client) Get(ctx context.Context, taskID string, options taskactivity.Options) (taskactivity.Activity, error) {
	answer, err := c.activity.GetTaskActivity(ctx, &operatorv1.GetTaskActivityRequest{TaskId: taskID, Options: activityOptions(options)})
	if err != nil {
		return taskactivity.Activity{}, fromStatus(c.address, err)
	}
	return activityOf(answer.GetActivity()), nil
}

// Watch follows the tree live; see taskactivity.Reader.
func (c *Client) Watch(ctx context.Context, taskID string, options taskactivity.Options, resume string, on func(cursor string, update taskactivity.Update) error) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	stream, err := c.activity.WatchTaskActivity(ctx, &operatorv1.WatchTaskActivityRequest{TaskId: taskID, Options: activityOptions(options), ResumeCursor: resume})
	if err != nil {
		return fromStatus(c.address, err)
	}
	for {
		response, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fromStatus(c.address, err)
		}
		if err := on(response.GetCursor(), updateOf(response)); err != nil {
			return err
		}
	}
}
