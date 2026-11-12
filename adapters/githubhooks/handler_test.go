package githubhooks

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/underpass-ai/axlr-bots/application"
)

var secret = []byte("a-hook-secret")

// fakePublisher keeps what was published by key, as the control plane
// does: the same key again is the event published before.
type fakePublisher struct {
	mu        sync.Mutex
	published map[string]application.PublishGroupEventCommand
	order     []string
	calls     int
	failOn    string
}

func (f *fakePublisher) PublishGroupEvent(_ context.Context, command application.PublishGroupEventCommand) (application.PublishAnswer, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if command.IdempotencyKey == f.failOn {
		return application.PublishAnswer{}, errors.New("the control plane is down")
	}
	if f.published == nil {
		f.published = map[string]application.PublishGroupEventCommand{}
	}
	_, before := f.published[command.IdempotencyKey]
	if !before {
		f.published[command.IdempotencyKey] = command
		f.order = append(f.order, command.IdempotencyKey)
	}
	return application.PublishAnswer{EventID: "ev-" + command.IdempotencyKey, Name: command.Name, Created: !before}, nil
}

// testRoute and testRepos are the operator's choices in these tests: the
// adapter has no default team, event, repository or branch.
var (
	testRoute = Route{Team: "builders", CIEvent: "pull-request.checked", MergedEvent: "pull-request.merged"}
	testRepos = []string{"example-org/repo", "example-org/other"}
)

func newTestHandler(t *testing.T, publisher Publisher, logs *bytes.Buffer) *Handler {
	t.Helper()
	h, err := NewHandler(Config{Secret: secret, Route: testRoute, Repos: testRepos, Publisher: publisher, Log: log.New(logs, "", 0)})
	if err != nil {
		t.Fatal(err)
	}
	return h
}

const delivery = "72d3162e-cc78-11e3-81ab-4c9367dc0958"

func post(h http.Handler, event string, body []byte, signature string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, Path, bytes.NewReader(body))
	r.Header.Set(HeaderEvent, event)
	r.Header.Set(HeaderDelivery, delivery)
	if signature != "" {
		r.Header.Set(HeaderSignature, signature)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func signed(h http.Handler, event, body string) *httptest.ResponseRecorder {
	return post(h, event, []byte(body), Sign(secret, []byte(body)))
}

func checkSuite(repo, action string, prs ...int) string {
	refs := make([]PullRequestRef, 0, len(prs))
	for _, n := range prs {
		refs = append(refs, PullRequestRef{Number: n})
	}
	raw, _ := json.Marshal(CheckSuiteEvent{Action: action, CheckSuite: CheckSuite{HeadSHA: "1a2b3c4d5e6f", Conclusion: "success", PullRequests: refs}, Repository: Repository{FullName: repo}})
	return string(raw)
}

func pullRequest(repo, action string, merged bool) string {
	raw, _ := json.Marshal(PullRequestEvent{Action: action, Number: 42, PullRequest: PullRequest{Merged: merged, MergeCommitSHA: "9f8e7d6c5b4a", Head: PullRequestHead{SHA: "1a2b3c4d5e6f"}}, Repository: Repository{FullName: repo}})
	return string(raw)
}

func TestTheSignature(t *testing.T) {
	var logs bytes.Buffer
	pub := &fakePublisher{}
	h := newTestHandler(t, pub, &logs)
	body := []byte(checkSuite("example-org/repo", "completed", 7))
	for name, signature := range map[string]string{
		"missing":     "",
		"other key":   Sign([]byte("another secret"), body),
		"no prefix":   strings.TrimPrefix(Sign(secret, body), "sha256="),
		"not hex":     "sha256=zz",
		"short":       "sha256=abcd",
		"other body":  Sign(secret, append(body, ' ')),
		"sha1 header": "sha1=" + strings.TrimPrefix(Sign(secret, body), "sha256="),
	} {
		if w := post(h, GitHubCheckSuite, body, signature); w.Code != http.StatusUnauthorized {
			t.Errorf("%s: %d", name, w.Code)
		}
	}
	if pub.calls != 0 {
		t.Errorf("a refused delivery published %d", pub.calls)
	}
	if strings.Contains(logs.String(), "1a2b3c4") || !strings.Contains(logs.String(), "refused, no signature") || !strings.Contains(logs.String(), "refused, bad signature") {
		t.Errorf("the log names the body or not the refusal:\n%s", logs.String())
	}
	if w := post(h, GitHubCheckSuite, body, Sign(secret, body)); w.Code != http.StatusAccepted || pub.calls != 1 {
		t.Errorf("good signature: %d, %d published", w.Code, pub.calls)
	}
}

func TestPingAndTheEventsItDoesNotUse(t *testing.T) {
	var logs bytes.Buffer
	pub := &fakePublisher{}
	h := newTestHandler(t, pub, &logs)
	for _, event := range []string{GitHubPing, "push", "issues", ""} {
		if w := signed(h, event, `{"zen": "Keep it logically awesome."}`); w.Code != http.StatusNoContent {
			t.Errorf("%q: %d", event, w.Code)
		}
	}
	if w := post(h, GitHubPing, []byte(`{}`), ""); w.Code != http.StatusUnauthorized {
		t.Errorf("an unsigned ping: %d", w.Code)
	}
	for name, body := range map[string]string{
		"suite requested":   checkSuite("example-org/repo", "requested", 7),
		"suite no PR":       checkSuite("example-org/repo", "completed"),
		"PR closed":         pullRequest("example-org/repo", "closed", false),
		"PR opened":         pullRequest("example-org/repo", "opened", false),
		"outside the list":  checkSuite("someone/else", "completed", 7),
		"PR outside a list": pullRequest("someone/else", "closed", true),
	} {
		event := GitHubCheckSuite
		if strings.HasPrefix(name, "PR") {
			event = GitHubPullRequest
		}
		if w := signed(h, event, body); w.Code != http.StatusNoContent {
			t.Errorf("%s: %d", name, w.Code)
		}
	}
	if pub.calls != 0 {
		t.Errorf("ignored deliveries published %d", pub.calls)
	}
	if !strings.Contains(logs.String(), `repository "someone/else" not in the allow-list`) || !strings.Contains(logs.String(), `event "push" ignored`) {
		t.Errorf("log:\n%s", logs.String())
	}
}

func TestACompletedSuitePublishesOnePRCIPerPullRequest(t *testing.T) {
	pub := &fakePublisher{}
	h := newTestHandler(t, pub, &bytes.Buffer{})
	if w := signed(h, GitHubCheckSuite, checkSuite("example-org/repo", "completed", 7)); w.Code != http.StatusAccepted {
		t.Fatalf("one PR: %d %s", w.Code, w.Body)
	}
	got := pub.published["github:"+delivery+":7"]
	want := application.PublishGroupEventCommand{Group: "builders", Name: "pull-request.checked", Message: "CI of example-org/repo#7 ended: success (head 1a2b3c4).", IdempotencyKey: "github:" + delivery + ":7"}
	if got.Group != want.Group || got.Name != want.Name || got.Message != want.Message || got.IdempotencyKey != want.IdempotencyKey {
		t.Errorf("command = %+v", got)
	}
	data, _ := json.Marshal(got.Data)
	if string(data) != `{"conclusion":"success","head_sha":"1a2b3c4d5e6f","number":7,"repo":"example-org/repo"}` {
		t.Errorf("data = %s", data)
	}
	pub = &fakePublisher{}
	h = newTestHandler(t, pub, &bytes.Buffer{})
	if w := signed(h, GitHubCheckSuite, checkSuite("Example-Org/Repo", "completed", 8, 9, 8)); w.Code != http.StatusAccepted || !strings.Contains(w.Body.String(), "published 2") {
		t.Fatalf("two PRs: %d %s", w.Code, w.Body)
	}
	if len(pub.order) != 2 || pub.order[0] != "github:"+delivery+":8" || pub.order[1] != "github:"+delivery+":9" {
		t.Errorf("keys = %v", pub.order)
	}
}

func TestAMergedPullRequestPublishesTheMergedEvent(t *testing.T) {
	pub := &fakePublisher{}
	h := newTestHandler(t, pub, &bytes.Buffer{})
	if w := signed(h, GitHubPullRequest, pullRequest("example-org/other", "closed", true)); w.Code != http.StatusAccepted {
		t.Fatalf("merged: %d %s", w.Code, w.Body)
	}
	got := pub.published["github:"+delivery+":42"]
	data, _ := json.Marshal(got.Data)
	if got.Name != "pull-request.merged" || got.Group != "builders" || got.Message != "example-org/other#42 was merged as 9f8e7d6 (head 1a2b3c4)." ||
		string(data) != `{"head_sha":"1a2b3c4d5e6f","merge_sha":"9f8e7d6c5b4a","number":42,"repo":"example-org/other"}` {
		t.Errorf("command = %+v %s", got, data)
	}
}

// A redelivery keeps its GUID: it names the same keys, and the control
// plane answers the events it published before.
func TestARedeliveryPublishesNothingTwice(t *testing.T) {
	pub := &fakePublisher{}
	h := newTestHandler(t, pub, &bytes.Buffer{})
	body := checkSuite("example-org/repo", "completed", 7, 8)
	for range 2 {
		if w := signed(h, GitHubCheckSuite, body); w.Code != http.StatusAccepted {
			t.Fatalf("%d %s", w.Code, w.Body)
		}
	}
	if pub.calls != 4 || len(pub.published) != 2 {
		t.Errorf("calls %d, events %d", pub.calls, len(pub.published))
	}
}

// A publish that fails is 502, so GitHub records the failure; the
// redelivery publishes what is missing and repeats what was not.
func TestAPublishFailureIsBadGateway(t *testing.T) {
	var logs bytes.Buffer
	pub := &fakePublisher{failOn: "github:" + delivery + ":8"}
	h := newTestHandler(t, pub, &logs)
	body := checkSuite("example-org/repo", "completed", 7, 8)
	if w := signed(h, GitHubCheckSuite, body); w.Code != http.StatusBadGateway {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if len(pub.published) != 1 || !strings.Contains(logs.String(), "pull-request.checked github:"+delivery+":8 not published: the control plane is down") {
		t.Errorf("published %v, log:\n%s", pub.order, logs.String())
	}
	pub.failOn = ""
	if w := signed(h, GitHubCheckSuite, body); w.Code != http.StatusAccepted {
		t.Fatalf("redelivery: %d", w.Code)
	}
	if len(pub.published) != 2 || pub.order[1] != "github:"+delivery+":8" || !strings.Contains(logs.String(), "(new false)") {
		t.Errorf("after the redelivery: %v", pub.order)
	}
}

func TestWhatIsNotADelivery(t *testing.T) {
	pub := &fakePublisher{}
	h := newTestHandler(t, pub, &bytes.Buffer{})
	big := bytes.Repeat([]byte("x"), MaxBody+1)
	if w := post(h, GitHubCheckSuite, big, Sign(secret, big)); w.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("too large: %d", w.Code)
	}
	r := httptest.NewRequest(http.MethodGet, Path, nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusMethodNotAllowed || w.Header().Get("Allow") != http.MethodPost {
		t.Errorf("GET: %d %v", w.Code, w.Header())
	}
	r = httptest.NewRequest(http.MethodPost, "/other", nil)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusNotFound {
		t.Errorf("other path: %d", w.Code)
	}
	for name, body := range map[string]string{"not json": `{`, "form": `payload=%7B%7D`, "wrong shape": `{"action": 3}`} {
		for _, event := range []string{GitHubCheckSuite, GitHubPullRequest} {
			if w := signed(h, event, body); w.Code != http.StatusBadRequest {
				t.Errorf("%s %s: %d", name, event, w.Code)
			}
		}
	}
	body := checkSuite("example-org/repo", "completed", 7)
	for _, id := range []string{"", "has space", strings.Repeat("a", 65), "../x"} {
		r := httptest.NewRequest(http.MethodPost, Path, strings.NewReader(body))
		r.Header.Set(HeaderEvent, GitHubCheckSuite)
		r.Header.Set(HeaderDelivery, id)
		r.Header.Set(HeaderSignature, Sign(secret, []byte(body)))
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != http.StatusBadRequest {
			t.Errorf("delivery %q: %d", id, w.Code)
		}
	}
	if pub.calls != 0 {
		t.Errorf("published %d", pub.calls)
	}
}

func TestNewHandlerRefuses(t *testing.T) {
	pub := &fakePublisher{}
	noCI, noMerged, badTeam := testRoute, testRoute, testRoute
	noCI.CIEvent, noMerged.MergedEvent, badTeam.Team = "", "Not An Event", "Not A Team"
	for name, config := range map[string]Config{
		"no secret":    {Route: testRoute, Repos: testRepos, Publisher: pub},
		"no team":      {Secret: secret, Route: Route{CIEvent: "a.b", MergedEvent: "c.d"}, Repos: testRepos, Publisher: pub},
		"bad team":     {Secret: secret, Route: badTeam, Repos: testRepos, Publisher: pub},
		"no ci event":  {Secret: secret, Route: noCI, Repos: testRepos, Publisher: pub},
		"bad merged":   {Secret: secret, Route: noMerged, Repos: testRepos, Publisher: pub},
		"no repos":     {Secret: secret, Route: testRoute, Publisher: pub},
		"no publisher": {Secret: secret, Route: testRoute, Repos: testRepos},
	} {
		if _, err := NewHandler(config); err == nil {
			t.Errorf("%s: wired", name)
		}
	}
	h, err := NewHandler(Config{Secret: secret, Route: testRoute, Publisher: pub, Repos: []string{"a/b"}, PublishTimeout: 1})
	if err != nil || h.log == nil || h.timeout != 1 || len(h.repos) != 1 {
		t.Errorf("%v %+v", err, h)
	}
	h, _ = NewHandler(Config{Secret: secret, Route: testRoute, Repos: testRepos, Publisher: pub})
	if h.timeout != DefaultPublishTimeout || len(h.repos) != len(testRepos) {
		t.Errorf("defaults %+v", h)
	}
}

func TestMappers(t *testing.T) {
	commands, _ := FromCheckSuite("d", testRoute, CheckSuiteEvent{Action: "completed", CheckSuite: CheckSuite{PullRequests: []PullRequestRef{{Number: 0}, {Number: 3}}}, Repository: Repository{FullName: "a/b"}})
	if len(commands) != 1 || commands[0].Message != "CI of a/b#3 ended: none (head unknown)." || commands[0].Data["conclusion"] != "" {
		t.Errorf("commands = %+v", commands)
	}
	if commands, _ := FromPullRequest("d", testRoute, PullRequestEvent{Action: "closed", Number: 0, PullRequest: PullRequest{Merged: true}}); len(commands) != 0 {
		t.Errorf("PR 0 = %+v", commands)
	}
	if short("abc") != "abc" || IdempotencyKey("g", 5) != "github:g:5" {
		t.Error("short or key")
	}
}

func TestParseRepos(t *testing.T) {
	for _, empty := range []string{"", " , ,"} {
		if repos, err := ParseRepos(empty); !errors.Is(err, ErrNoRepos) || repos != nil {
			t.Errorf("%q = %v %v: the operator names the repositories", empty, repos, err)
		}
	}
	repos, err := ParseRepos(" a/b , A/B,c/d,")
	if err != nil || len(repos) != 2 {
		t.Errorf("list = %v %v", repos, err)
	}
	for _, bad := range []string{"a", "a/b/c", "a/..", "a b/c", "/b"} {
		if _, err := ParseRepos(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

// Only the branches with the route's prefix wake a bot: a person's PR
// maps to none; an empty prefix maps every branch.
func TestOnlyTheBranchesWithThePrefixMap(t *testing.T) {
	suite := func(branch string) CheckSuiteEvent {
		return CheckSuiteEvent{Action: "completed", Repository: Repository{FullName: "example-org/repo"},
			CheckSuite: CheckSuite{HeadSHA: "abc", HeadBranch: branch, Conclusion: "success", PullRequests: []PullRequestRef{{Number: 7}}}}
	}
	merged := func(branch string) PullRequestEvent {
		return PullRequestEvent{Action: "closed", Number: 7, Repository: Repository{FullName: "example-org/repo"},
			PullRequest: PullRequest{Merged: true, MergeCommitSHA: "def", Head: PullRequestHead{SHA: "abc", Ref: branch}}}
	}
	prefixed := testRoute
	prefixed.BranchPrefix = "bot/"
	for branch, want := range map[string]int{"bot/repo/fix-200": 1, "someone/fix": 0, "main": 0, "": 0, "botx/repo": 0} {
		got, err := FromCheckSuite("d-1", prefixed, suite(branch))
		if err != nil || len(got) != want {
			t.Errorf("check suite on %q: %d %v, want %d", branch, len(got), err, want)
		}
		got, err = FromPullRequest("d-1", prefixed, merged(branch))
		if err != nil || len(got) != want {
			t.Errorf("merge of %q: %d %v, want %d", branch, len(got), err, want)
		}
	}
	if got, _ := FromCheckSuite("d-1", testRoute, suite("someone/fix")); len(got) != 1 {
		t.Error("an empty prefix maps every branch")
	}
}
