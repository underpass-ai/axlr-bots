package githubhooks

import "context"

// HookAPI is the port to GitHub's REST API of a repository's hooks that
// redeliver uses (GH is its adapter over the gh CLI).
type HookAPI interface {
	// Hooks lists the repository's hooks.
	Hooks(ctx context.Context, repo string) ([]Hook, error)
	// Deliveries lists every attempt GitHub keeps of the hook's deliveries.
	Deliveries(ctx context.Context, repo string, hook int) ([]HookDelivery, error)
	// Redeliver asks GitHub to attempt the delivery again.
	Redeliver(ctx context.Context, repo string, hook, delivery int) error
}
