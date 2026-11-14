// Package taskactivity is what an operator sees of everything that
// happened under a task: the task, the tasks it delegated, each
// execution with what it used, the timeline, and where the progress
// timeline was cut, the cut and the content completed from the
// transcript. Activity is the read model; Reader is the port an
// adapter implements (the gRPC client over ActivityService); Update and
// Activity.Apply follow the tree live.
package taskactivity

// Options bound what an answer holds. The zero value asks for
// everything with the server's defaults.
type Options struct {
	// ExcludeChildren answers the task alone.
	ExcludeChildren bool
	// MaxDepth bounds the levels of children followed; zero is the
	// delegation limit.
	MaxDepth int
	// SkipCompletedProgress leaves out the content completed from
	// transcripts.
	SkipCompletedProgress bool
	// MaxCompletedItems bounds the items completed per execution; zero
	// is the server's default.
	MaxCompletedItems int
}
