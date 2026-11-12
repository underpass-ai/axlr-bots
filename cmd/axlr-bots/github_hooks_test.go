package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/underpass-ai/axlr-bots/adapters/githubhooks"
	"github.com/underpass-ai/axlr-bots/adapters/unixapi"
	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

func hookSecretFile(t *testing.T, mode os.FileMode, text string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "hook.secret")
	if err := os.WriteFile(path, []byte(text), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestGitHubHooksServeRefuses(t *testing.T) {
	good := hookSecretFile(t, 0o600, "s3cret\n")
	socket := filepath.Join(t.TempDir(), "cp.sock")
	for name, c := range map[string]struct {
		socket string
		args   []string
		code   int
		want   string
	}{
		"no secret":       {socket, nil, exitUsage, "needs --secret-file"},
		"extra":           {socket, []string{"--secret-file", good, "extra"}, exitUsage, "needs --secret-file"},
		"bad flag":        {socket, []string{"--bogus"}, exitUsage, ""},
		"group readable":  {socket, []string{"--secret-file", hookSecretFile(t, 0o640, "s")}, exitError, "readable by its group or others"},
		"world readable":  {socket, []string{"--secret-file", hookSecretFile(t, 0o604, "s")}, exitError, "chmod 600"},
		"empty secret":    {socket, []string{"--secret-file", hookSecretFile(t, 0o600, "\n")}, exitError, "is empty"},
		"missing secret":  {socket, []string{"--secret-file", filepath.Join(t.TempDir(), "none")}, exitError, "no such file"},
		"a directory":     {socket, []string{"--secret-file", t.TempDir()}, exitError, "not a regular file"},
		"every interface": {socket, []string{"--secret-file", good, "--listen", ":7890"}, exitError, "not a loopback address; give --public"},
		"0.0.0.0":         {socket, []string{"--secret-file", good, "--listen", "0.0.0.0:7890"}, exitError, "not a loopback address"},
		"a LAN address":   {socket, []string{"--secret-file", good, "--listen", "192.168.1.10:7890"}, exitError, "not a loopback address"},
		"no port":         {socket, []string{"--secret-file", good, "--listen", "127.0.0.1"}, exitError, "missing port"},
		"bad team":        {socket, []string{"--secret-file", good, "--listen", "127.0.0.1:0", "--team", "Not A Team"}, exitError, "team"},
		"bad repos":       {socket, []string{"--secret-file", good, "--listen", "127.0.0.1:0", "--repos", "nope"}, exitError, "not a repository"},
		"no socket":       {"", []string{"--secret-file", good, "--listen", "127.0.0.1:0"}, exitError, "--socket is empty"},
		"port taken":      {socket, []string{"--secret-file", good, "--listen", "127.0.0.1:-1"}, exitError, "invalid port"},
	} {
		var stderr bytes.Buffer
		code := githubHooksServe(context.Background(), c.socket, c.args, &stderr)
		if code != c.code || !strings.Contains(stderr.String(), c.want) {
			t.Errorf("%s: exit %d, %s", name, code, stderr.String())
		}
	}
	for addr, public := range map[string]bool{"127.0.0.1:1": false, "[::1]:1": false, "localhost:1": false, "0.0.0.0:1": true} {
		if err := loopbackOnly(addr, public); err != nil {
			t.Errorf("%s: %v", addr, err)
		}
	}
	var stdout, stderr bytes.Buffer
	if code := run([]string{"--socket", socket, "github-hooks"}, &stdout, &stderr); code != exitUsage || !strings.Contains(stderr.String(), "serve or redeliver") {
		t.Errorf("no verb: %d %s", code, stderr.String())
	}
	if code := run([]string{"--socket", socket, "github-hooks", "serve"}, &stdout, &stderr); code != exitUsage {
		t.Errorf("serve through run: %d", code)
	}
	if code := run([]string{"github-hooks", "redeliver", "--since", "0s"}, &stdout, &stderr); code != exitUsage {
		t.Errorf("redeliver through run: %d", code)
	}
}

// The adapter against a real control plane: a merged PR and a finished CI
// reach team dev as pr.mergeada and pr.ci, published as the operator; a
// redelivery publishes nothing new; a bad signature is refused; with the
// daemon gone a delivery is 502.
func TestGitHubHooksServePublishesToTheTeam(t *testing.T) {
	state := t.TempDir()
	socket := filepath.Join(t.TempDir(), "cp.sock")
	c := cli{t: t, state: state, axlr: fakeAXLR(t, "completed")}
	raw, err := os.ReadFile(filepath.Join("testdata", "bot-team-a.json"))
	if err != nil {
		t.Fatal(err)
	}
	definition := filepath.Join(t.TempDir(), "dev-bot.json")
	_ = os.WriteFile(definition, bytes.ReplaceAll(bytes.ReplaceAll(raw, []byte(`"releases"`), []byte(`"dev"`)), []byte(`"team-a"`), []byte(`"dev-a"`)), 0o644)
	if code, _, errText := c.run("bot", "register", "--file", definition); code != exitOK {
		t.Fatalf("register: %s", errText)
	}
	daemonCtx, stopDaemon := context.WithCancel(context.Background())
	daemonDone := make(chan int, 1)
	var daemonOut, daemonErr syncBuffer
	go func() {
		daemonDone <- serve(daemonCtx, mustApp(t, state, c.axlr), state, socket, nil, &daemonOut, &daemonErr)
	}()
	for !unixapi.Reachable(context.Background(), socket) {
		time.Sleep(10 * time.Millisecond)
	}
	secret := hookSecretFile(t, 0o600, "the-hook-secret\n")
	hooksCtx, stopHooks := context.WithCancel(context.Background())
	hooksDone := make(chan int, 1)
	var hooksErr syncBuffer
	go func() {
		hooksDone <- githubHooksServe(hooksCtx, socket, []string{"--secret-file", secret, "--listen", "127.0.0.1:0"}, &hooksErr)
	}()
	listening := regexp.MustCompile(`listening on (http://\S+/github)`)
	var url string
	for deadline := time.Now().Add(10 * time.Second); url == ""; {
		if m := listening.FindStringSubmatch(hooksErr.String()); m != nil {
			url = m[1]
		} else if time.Now().After(deadline) {
			t.Fatalf("not listening: %s", hooksErr.String())
		}
		time.Sleep(10 * time.Millisecond)
	}
	send := func(event, delivery, body string, signature string) int {
		t.Helper()
		request, _ := http.NewRequest(http.MethodPost, url, strings.NewReader(body))
		request.Header.Set(githubhooks.HeaderEvent, event)
		request.Header.Set(githubhooks.HeaderDelivery, delivery)
		request.Header.Set(githubhooks.HeaderSignature, signature)
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		return response.StatusCode
	}
	sign := func(body string) string { return githubhooks.Sign([]byte("the-hook-secret"), []byte(body)) }
	suite := `{"action": "completed", "check_suite": {"head_sha": "1a2b3c4d5e6f", "head_branch": "dev/kmp/a-20261112-kmp-7", "conclusion": "failure", "pull_requests": [{"number": 7}, {"number": 8}]}, "repository": {"full_name": "underpass-ai/kmp"}}`
	merged := `{"action": "closed", "number": 7, "pull_request": {"merged": true, "merge_commit_sha": "9f8e7d6c5b4a", "head": {"sha": "1a2b3c4d5e6f", "ref": "dev/axlr-bots/a-20261112-axlr-bots-7"}}, "repository": {"full_name": "underpass-ai/axlr-bots"}}`
	if code := send("check_suite", "d-1", suite, sign(suite)); code != http.StatusAccepted {
		t.Fatalf("check_suite: %d\n%s", code, hooksErr.String())
	}
	if code := send("pull_request", "d-2", merged, sign(merged)); code != http.StatusAccepted {
		t.Fatalf("pull_request: %d\n%s", code, hooksErr.String())
	}
	if code := send("check_suite", "d-1", suite, sign(suite)); code != http.StatusAccepted {
		t.Fatalf("redelivery: %d", code)
	}
	if code := send("check_suite", "d-3", suite, sign(suite+" ")); code != http.StatusUnauthorized {
		t.Errorf("bad signature: %d", code)
	}
	client := unixapi.NewClient(socket)
	events, err := client.ListGroupEvents(context.Background(), application.GroupEventsQuery{Name: "dev", Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	var published []domain.Event
	for _, event := range events {
		if event.Type == domain.EventGroupEventPublished {
			published = append(published, event)
		}
	}
	if len(published) != 3 {
		t.Fatalf("published %d events on dev: %+v", len(published), published)
	}
	names := []string{}
	for _, event := range published {
		names = append(names, event.Payload["name"].(string))
		if event.Payload["by"] != "operator" {
			t.Errorf("published by %v", event.Payload["by"])
		}
	}
	if strings.Join(names, " ") != "pr.ci pr.ci pr.mergeada" {
		t.Errorf("names = %v", names)
	}
	data, _ := json.Marshal(published[2].Payload["data"])
	if !strings.Contains(string(data), `"merge_sha":"9f8e7d6c5b4a"`) || !strings.Contains(string(data), `"number":7`) {
		t.Errorf("pr.mergeada data = %s", data)
	}
	stopDaemon()
	<-daemonDone
	if code := send("pull_request", "d-4", merged, sign(merged)); code != http.StatusBadGateway {
		t.Errorf("daemon down: %d", code)
	}
	stopHooks()
	if code := <-hooksDone; code != exitOK {
		t.Errorf("serve exit %d: %s", code, hooksErr.String())
	}
	if !strings.Contains(hooksErr.String(), "refused, bad signature") || !strings.Contains(hooksErr.String(), "pr.mergeada github:d-4:7 not published") {
		t.Errorf("log:\n%s", hooksErr.String())
	}
}

// fakeHookAPI is GitHub's hooks API with one failed delivery.
type fakeHookAPI struct {
	asked []int
	fail  bool
}

func (f *fakeHookAPI) Hooks(context.Context, string) ([]githubhooks.Hook, error) {
	if f.fail {
		return nil, errors.New("gh: not signed in")
	}
	return []githubhooks.Hook{{ID: 1, Active: true, Config: githubhooks.HookConfig{URL: "https://gateway.example:8443/github"}}}, nil
}

func (f *fakeHookAPI) Deliveries(context.Context, string, int) ([]githubhooks.HookDelivery, error) {
	return []githubhooks.HookDelivery{
		{ID: 10, GUID: "a", DeliveredAt: "2026-11-12T11:00:00Z", StatusCode: 0},
		{ID: 20, GUID: "b", DeliveredAt: "2026-11-12T11:00:00Z", StatusCode: 502},
		{ID: 21, GUID: "b", DeliveredAt: "2026-11-12T11:30:00Z", StatusCode: 202},
	}, nil
}

func (f *fakeHookAPI) Redeliver(_ context.Context, _ string, _, delivery int) error {
	f.asked = append(f.asked, delivery)
	return nil
}

func TestGitHubHooksRedeliver(t *testing.T) {
	now := time.Date(2026, 11, 12, 12, 0, 0, 0, time.UTC)
	api := &fakeHookAPI{}
	var stdout, stderr bytes.Buffer
	if code := githubHooksRedeliver(context.Background(), api, now, []string{"--repos", "underpass-ai/kmp", "--since", "2h"}, &stdout, &stderr); code != exitOK {
		t.Fatalf("exit %d: %s", code, stderr.String())
	}
	var out []githubhooks.Redelivery
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil || len(out) != 1 || out[0].GUID != "a" || len(api.asked) != 1 || api.asked[0] != 10 {
		t.Errorf("out %s, asked %v", stdout.String(), api.asked)
	}
	stdout.Reset()
	if code := githubHooksRedeliver(context.Background(), &fakeHookAPI{}, now, []string{"--repos", "underpass-ai/kmp", "--since", "30m"}, &stdout, &stderr); code != exitOK || strings.TrimSpace(stdout.String()) != "[]" {
		t.Errorf("nothing newer: %d %s", code, stdout.String())
	}
	if code := githubHooksRedeliver(context.Background(), &fakeHookAPI{fail: true}, now, nil, &stdout, &stderr); code != exitError || !strings.Contains(stderr.String(), "not signed in") {
		t.Errorf("a failing gh: %d %s", code, stderr.String())
	}
	for _, args := range [][]string{{"extra"}, {"--bogus"}, {"--since", "-1h"}} {
		if code := githubHooksRedeliver(context.Background(), api, now, args, &stdout, &stderr); code != exitUsage {
			t.Errorf("%v: %d", args, code)
		}
	}
	if code := githubHooksRedeliver(context.Background(), api, now, []string{"--repos", "x"}, &stdout, &stderr); code != exitError {
		t.Errorf("bad repos: %d", code)
	}
	// The real gh adapter is the default: a missing binary is an error.
	if code := githubHooksRedeliver(context.Background(), nil, now, []string{"--repos", "a/b", "--gh", filepath.Join(t.TempDir(), "none")}, &stdout, &stderr); code != exitError {
		t.Errorf("missing gh: %d", code)
	}
}
