package taskactivity

import "context"

// Reader reads a task's activity: as it is now, or following it live.
type Reader interface {
	// Get answers the tree as it is now.
	Get(ctx context.Context, taskID string, options Options) (Activity, error)
	// Watch calls on with the snapshot and then every change, in order,
	// with the cursor of each, until ctx ends, on returns an error, or
	// the server ends the stream: nil then, the whole tree finished. A
	// resume cursor that names the last cursor seen asks for updates
	// instead of a second snapshot.
	Watch(ctx context.Context, taskID string, options Options, resume string, on func(cursor string, update Update) error) error
}
