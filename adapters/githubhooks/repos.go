package githubhooks

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// ErrNoRepos is an allow-list that names no repository: the operator
// names the repositories, the platform names none.
var ErrNoRepos = errors.New("githubhooks: no repositories: name them, owner/name comma-separated")

var repoPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,100}/[A-Za-z0-9_.-]{1,100}$`)

// ParseRepos reads a comma-separated allow-list of owner/name; empty is
// ErrNoRepos.
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
		return nil, ErrNoRepos
	}
	return repos, nil
}

// allowed reports whether repo is in the list, as GitHub compares names:
// without case.
func allowed(repos []string, repo string) bool {
	return slices.ContainsFunc(repos, func(r string) bool { return strings.EqualFold(r, repo) })
}
