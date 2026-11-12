package githubhooks

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/underpass-ai/axlr-bots/application"
)

// The team events the adapter publishes: the spec's PRCIEvent and
// PRMergeadaEvent names.
const (
	EventPRCI       = "pr.ci"
	EventPRMergeada = "pr.mergeada"
)

// The actions that map; any other is ignored.
const (
	actionCompleted = "completed"
	actionClosed    = "closed"
)

var deliveryPattern = regexp.MustCompile(`^[A-Za-z0-9-]{1,64}$`)

// IdempotencyKey is the key of the event a delivery publishes for a pull
// request: github:<delivery>:<number>. A redelivery keeps its GUID, so it
// names the same keys and publishes nothing twice.
func IdempotencyKey(delivery string, number int) string {
	return fmt.Sprintf("github:%s:%d", delivery, number)
}

// DefaultBranchPrefix is the dev team's own branches (dev/<product>/<fix>):
// a webhook of any other branch (a person's PR) wakes no bot.
const DefaultBranchPrefix = "dev/"

// ownBranch reports whether the branch is one the prefix maps; an empty
// prefix maps every branch.
func ownBranch(prefix, branch string) bool {
	return prefix == "" || strings.HasPrefix(branch, prefix)
}

// FromCheckSuite maps a completed check suite of a branch with the prefix
// to one pr.ci per pull request it names, each once; another action or
// branch maps to none.
func FromCheckSuite(delivery, team, prefix string, event CheckSuiteEvent) ([]application.PublishGroupEventCommand, error) {
	if event.Action != actionCompleted || !ownBranch(prefix, event.CheckSuite.HeadBranch) {
		return nil, nil
	}
	repo, suite := event.Repository.FullName, event.CheckSuite
	var commands []application.PublishGroupEventCommand
	seen := map[int]bool{}
	for _, pr := range suite.PullRequests {
		if pr.Number < 1 || seen[pr.Number] {
			continue
		}
		seen[pr.Number] = true
		conclusion := suite.Conclusion
		if conclusion == "" {
			conclusion = "none"
		}
		data, err := dataOf(PRCIData{Repo: repo, Number: pr.Number, HeadSHA: suite.HeadSHA, Conclusion: suite.Conclusion})
		if err != nil {
			return nil, err
		}
		commands = append(commands, application.PublishGroupEventCommand{
			Group:          team,
			Name:           EventPRCI,
			Message:        fmt.Sprintf("CI of %s#%d ended: %s (head %s).", repo, pr.Number, conclusion, short(suite.HeadSHA)),
			Data:           data,
			IdempotencyKey: IdempotencyKey(delivery, pr.Number),
		})
	}
	return commands, nil
}

// FromPullRequest maps a pull request of a branch with the prefix closed
// merged to pr.mergeada; anything else maps to none.
func FromPullRequest(delivery, team, prefix string, event PullRequestEvent) ([]application.PublishGroupEventCommand, error) {
	if event.Action != actionClosed || !event.PullRequest.Merged || event.Number < 1 || !ownBranch(prefix, event.PullRequest.Head.Ref) {
		return nil, nil
	}
	repo, pr := event.Repository.FullName, event.PullRequest
	data, err := dataOf(PRMergeadaData{Repo: repo, Number: event.Number, MergeSHA: pr.MergeCommitSHA, HeadSHA: pr.Head.SHA})
	if err != nil {
		return nil, err
	}
	return []application.PublishGroupEventCommand{{
		Group:          team,
		Name:           EventPRMergeada,
		Message:        fmt.Sprintf("%s#%d was merged as %s (head %s).", repo, event.Number, short(pr.MergeCommitSHA), short(pr.Head.SHA)),
		Data:           data,
		IdempotencyKey: IdempotencyKey(delivery, event.Number),
	}}, nil
}

// dataOf is a generated DTO as the command's data: its keys are the
// spec's, never written here.
func dataOf(dto any) (map[string]any, error) {
	raw, err := json.Marshal(dto)
	if err != nil {
		return nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var data map[string]any
	if err := dec.Decode(&data); err != nil {
		return nil, err
	}
	return data, nil
}

// short is a commit's first seven characters, as GitHub shows it.
func short(sha string) string {
	if sha == "" {
		return "unknown"
	}
	return sha[:min(7, len(sha))]
}
