package githubhooks

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeAPI is GitHub's hooks API in memory.
type fakeAPI struct {
	hooks       map[string][]Hook
	deliveries  map[int][]HookDelivery
	failHooks   string
	failList    int
	failAttempt int
	attempts    []string
}

func (f *fakeAPI) Hooks(_ context.Context, repo string) ([]Hook, error) {
	if repo == f.failHooks {
		return nil, errors.New("not found")
	}
	return f.hooks[repo], nil
}

func (f *fakeAPI) Deliveries(_ context.Context, _ string, hook int) ([]HookDelivery, error) {
	if hook == f.failList {
		return nil, errors.New("forbidden")
	}
	return f.deliveries[hook], nil
}

func (f *fakeAPI) Redeliver(_ context.Context, repo string, hook, delivery int) error {
	if delivery == f.failAttempt {
		return errors.New("rate limited")
	}
	f.attempts = append(f.attempts, fmt.Sprintf("%s/%d/%d", repo, hook, delivery))
	return nil
}

var now = time.Date(2026, 11, 12, 12, 0, 0, 0, time.UTC)

func at(ago time.Duration) string { return now.Add(-ago).Format(time.RFC3339) }

func TestRedeliverPicksOnlyTheFailedWithoutASuccess(t *testing.T) {
	api := &fakeAPI{
		hooks: map[string][]Hook{
			"underpass-ai/kmp": {
				{ID: 1, Active: true, Config: HookConfig{URL: "https://gateway.example:8443/github"}},
				{ID: 2, Active: true, Config: HookConfig{URL: "https://ci.example/hook"}},
				{ID: 3, Active: false, Config: HookConfig{URL: "https://old.example/github"}},
			},
		},
		deliveries: map[int][]HookDelivery{
			1: {
				{ID: 10, GUID: "a", DeliveredAt: at(2 * time.Hour), StatusCode: 0, Event: "check_suite"},
				{ID: 11, GUID: "a", DeliveredAt: at(time.Hour), StatusCode: 502, Redelivery: true, Event: "check_suite"},
				{ID: 20, GUID: "b", DeliveredAt: at(3 * time.Hour), StatusCode: 502},
				{ID: 21, GUID: "b", DeliveredAt: at(2 * time.Hour), StatusCode: 202, Redelivery: true},
				{ID: 30, GUID: "c", DeliveredAt: at(30 * time.Hour), StatusCode: 0},
				{ID: 40, GUID: "d", DeliveredAt: at(time.Minute), StatusCode: 204},
				{ID: 50, GUID: "e", DeliveredAt: at(3 * time.Hour), StatusCode: 401, Event: "pull_request"},
				{ID: 60, GUID: "", DeliveredAt: at(time.Hour), StatusCode: 0},
				{ID: 70, GUID: "f", DeliveredAt: "yesterday", StatusCode: 0},
			},
			2: {{ID: 80, GUID: "g", DeliveredAt: at(time.Hour), StatusCode: 500}},
			3: {{ID: 90, GUID: "h", DeliveredAt: at(time.Hour), StatusCode: 500}},
		},
	}
	got, err := Redeliver(context.Background(), api, RedeliverOptions{Repos: []string{"underpass-ai/kmp", "underpass-ai/made"}, Since: 24 * time.Hour, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(api.attempts, " ") != "underpass-ai/kmp/1/50 underpass-ai/kmp/1/11" {
		t.Errorf("attempts = %v", api.attempts)
	}
	if len(got) != 2 || got[1] != (Redelivery{Repo: "underpass-ai/kmp", Hook: 1, Delivery: 11, GUID: "a", Event: "check_suite", StatusCode: 502, DeliveredAt: at(time.Hour)}) {
		t.Errorf("redelivered = %+v", got)
	}
	api.attempts = nil
	if _, err := Redeliver(context.Background(), api, RedeliverOptions{Repos: []string{"underpass-ai/kmp"}, HookURL: "https://ci.example/hook", Since: 24 * time.Hour, Now: now}); err != nil || strings.Join(api.attempts, " ") != "underpass-ai/kmp/2/80" {
		t.Errorf("--hook-url: %v %v", api.attempts, err)
	}
}

func TestRedeliverGoesOnPastErrors(t *testing.T) {
	api := &fakeAPI{
		hooks: map[string][]Hook{
			"a/one":   {{ID: 1, Active: true, Config: HookConfig{URL: "https://x/github"}}, {ID: 2, Active: true, Config: HookConfig{URL: "https://y/github"}}},
			"a/three": {{ID: 3, Active: true, Config: HookConfig{URL: "https://z/github"}}},
		},
		deliveries: map[int][]HookDelivery{
			2: {{ID: 20, GUID: "x", DeliveredAt: at(time.Hour)}, {ID: 21, GUID: "y", DeliveredAt: at(time.Hour)}},
			3: {{ID: 30, GUID: "z", DeliveredAt: at(time.Hour), StatusCode: 503}},
		},
		failHooks: "a/two", failList: 1, failAttempt: 20,
	}
	got, err := Redeliver(context.Background(), api, RedeliverOptions{Repos: []string{"a/one", "a/two", "a/three"}, Since: 24 * time.Hour, Now: now})
	if len(got) != 2 || err == nil {
		t.Fatalf("%+v %v", got, err)
	}
	for _, want := range []string{"a/two: not found", "a/one hook 1: forbidden", "a/one hook 2 delivery 20: rate limited"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error lacks %q: %v", want, err)
		}
	}
	if ours("://bad", "") {
		t.Error("a bad URL is ours")
	}
}

// fakeGH writes a gh that answers from canned files by its arguments.
func fakeGH(t *testing.T, script string) GH {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "gh")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+script), 0o755); err != nil {
		t.Fatal(err)
	}
	return GH{Bin: path}
}

func TestGHReadsThePagesAndRedelivers(t *testing.T) {
	log := filepath.Join(t.TempDir(), "args")
	gh := fakeGH(t, `echo "$@" >> `+log+`
case "$3" in
  repos/a/b/hooks?per_page=100) echo '[{"id": 1, "active": true, "config": {"url": "https://x/github", "secret": "********"}, "events": ["check_suite"]}]' ;;
  repos/a/b/hooks/1/deliveries?per_page=100) echo '[{"id": 10, "guid": "g", "delivered_at": "2026-11-12T10:00:00Z", "status_code": 0, "status": "Timed out"}][{"id": 11, "guid": "h", "delivered_at": "2026-11-12T11:00:00Z", "status_code": 202}]' ;;
  repos/a/b/hooks/9/deliveries?per_page=100) echo '[{"id": "x"}]' ;;
  repos/a/b/hooks/8/deliveries?per_page=100) echo '{' ;;
esac
case "$4" in repos/a/b/hooks/1/deliveries/10/attempts) exit 0 ;; esac
[ "$2" = --paginate ] || { echo "HTTP 404" >&2; exit 1; }
`)
	ctx := context.Background()
	hooks, err := gh.Hooks(ctx, "a/b")
	if err != nil || len(hooks) != 1 || hooks[0].ID != 1 || !hooks[0].Active || hooks[0].Config.URL != "https://x/github" || hooks[0].Events[0] != "check_suite" {
		t.Fatalf("hooks = %+v %v", hooks, err)
	}
	deliveries, err := gh.Deliveries(ctx, "a/b", 1)
	if err != nil || len(deliveries) != 2 || deliveries[0].GUID != "g" || deliveries[0].Status != "Timed out" || deliveries[1].StatusCode != 202 {
		t.Fatalf("deliveries = %+v %v", deliveries, err)
	}
	if err := gh.Redeliver(ctx, "a/b", 1, 10); err != nil {
		t.Errorf("redeliver: %v", err)
	}
	if err := gh.Redeliver(ctx, "a/b", 1, 99); err == nil || !strings.Contains(err.Error(), "HTTP 404") {
		t.Errorf("a failed redeliver: %v", err)
	}
	for _, hook := range []int{9, 8} {
		if _, err := gh.Deliveries(ctx, "a/b", hook); err == nil {
			t.Errorf("hook %d: a bad answer read", hook)
		}
	}
	args, _ := os.ReadFile(log)
	if !strings.Contains(string(args), "api --method POST repos/a/b/hooks/1/deliveries/10/attempts --silent") {
		t.Errorf("gh was called with:\n%s", args)
	}
	if _, err := (GH{Bin: filepath.Join(t.TempDir(), "none")}).Hooks(ctx, "a/b"); err == nil {
		t.Error("a missing gh answered")
	}
	if (GH{}).bin() != "gh" {
		t.Error("the default binary")
	}
}
