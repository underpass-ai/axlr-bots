package githubhooks

// The headers of a GitHub delivery the adapter reads: the spec's
// WebhookHeaders (the drift test checks the names).
const (
	HeaderEvent     = "X-GitHub-Event"
	HeaderDelivery  = "X-GitHub-Delivery"
	HeaderSignature = "X-Hub-Signature-256"
)

// The events of X-GitHub-Event the adapter answers; any other is ignored.
const (
	GitHubCheckSuite  = "check_suite"
	GitHubPullRequest = "pull_request"
	GitHubPing        = "ping"
)
