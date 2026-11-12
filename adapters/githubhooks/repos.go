package githubhooks

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// DefaultRepos are the products whose hooks the adapter maps when --repos
// is not given.
var DefaultRepos = []string{"underpass-ai/kmp", "underpass-ai/made", "underpass-ai/axlr", "underpass-ai/axlr-bots"}

var repoPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,100}/[A-Za-z0-9_.-]{1,100}$`)

// ParseRepos reads a comma-separated allow-list of owner/name; empty is
// DefaultRepos.
func ParseRepos(raw string) ([]string, error) {
	var repos []string
	for _, repo := range strings.Split(raw, ",") {
		repo = strings.TrimSpace(repo)
		if repo == "" {
			continue
		}
		if !repoPattern.MatchString(repo) || strings.Contains(repo, "..") {
			return nil, fmt.Errorf("githubhooks: %q is not a repository owner/name", repo)
		}
		if !slices.ContainsFunc(repos, func(r string) bool { return strings.EqualFold(r, repo) }) {
			repos = append(repos, repo)
		}
	}
	if len(repos) == 0 {
		return slices.Clone(DefaultRepos), nil
	}
	return repos, nil
}

// allowed reports whether repo is in the list, as GitHub compares names:
// without case.
func allowed(repos []string, repo string) bool {
	return slices.ContainsFunc(repos, func(r string) bool { return strings.EqualFold(r, repo) })
}
