package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/underpass-ai/axlr-bots/adapters/githubhooks"
	"github.com/underpass-ai/axlr-bots/domain"
)

// githubHooks runs the GitHub webhook adapter (docs/runbooks/github-hooks.md).
// It never opens the state directory: it publishes through the daemon's
// socket, or its operator address with --control, so a daemon that is
// down is a 502 GitHub can redeliver.
func githubHooks(socketPath string, remote remoteControl, args []string, stdout, stderr io.Writer) int {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	verb := ""
	if len(args) > 0 {
		verb, args = args[0], args[1:]
	}
	switch verb {
	case "serve":
		return githubHooksServe(ctx, socketPath, remote, args, stderr)
	case "redeliver":
		return githubHooksRedeliver(ctx, nil, time.Now(), args, stdout, stderr)
	}
	fmt.Fprintln(stderr, "axlr-bots: github-hooks needs serve or redeliver")
	return exitUsage
}

// githubHooksServe serves POST /github until ctx ends.
func githubHooksServe(ctx context.Context, socketPath string, remote remoteControl, args []string, stderr io.Writer) int {
	flags := flag.NewFlagSet("github-hooks serve", flag.ContinueOnError)
	flags.SetOutput(stderr)
	logFormat := logFormatFlag(flags)
	listen := flags.String("listen", "127.0.0.1:7890", "where to listen: a loopback address, the tunnel's far end")
	secretFile := flags.String("secret-file", "", "the hook's secret, a file readable by its owner only")
	team := flags.String("team", "", "the team the events are published to (required)")
	ciEvent := flags.String("ci-event", "", "the name of the event published when a pull request's CI ends (required)")
	mergedEvent := flags.String("merged-event", "", "the name of the event published when a pull request is merged (required)")
	repos := flags.String("repos", "", "the repositories mapped, owner/name comma-separated (required)")
	branchPrefix := flags.String("branch-prefix", "", "map only the branches that start with it (default empty: every branch)")
	public := flags.Bool("public", false, "allow a listen address that is not loopback")
	positional, ok := parseAnywhere(flags, args)
	if !ok {
		return exitUsage
	}
	if len(positional) != 0 || *secretFile == "" || *team == "" || *ciEvent == "" || *mergedEvent == "" || *repos == "" {
		fmt.Fprintln(stderr, "axlr-bots: github-hooks serve needs --secret-file <path> --team <team> --ci-event <name> --merged-event <name> --repos a/b,c/d")
		return exitUsage
	}
	if err := loopbackOnly(*listen, *public); err != nil {
		return fail(stderr, err)
	}
	secret, err := readHookSecret(*secretFile)
	if err != nil {
		return fail(stderr, err)
	}
	if _, err := domain.NewTeamName(*team); err != nil {
		return fail(stderr, err)
	}
	for _, name := range []string{*ciEvent, *mergedEvent} {
		if _, err := domain.NewEventName(name); err != nil {
			return fail(stderr, err)
		}
	}
	allow, err := githubhooks.ParseRepos(*repos)
	if err != nil {
		return fail(stderr, err)
	}
	if socketPath == "" && !remote.enabled() {
		return fail(stderr, errors.New("github-hooks serve publishes through the daemon's socket; --socket is empty"))
	}
	publisher, err := remote.clientOr(socketPath)
	if err != nil {
		return fail(stderr, err)
	}
	loggers, err := newLoggers(stderr, *logFormat)
	if err != nil {
		return fail(stderr, err)
	}
	logger := loggers.Info
	handler, err := githubhooks.NewHandler(githubhooks.Config{Secret: secret, Route: githubhooks.Route{Team: *team, CIEvent: *ciEvent, MergedEvent: *mergedEvent, BranchPrefix: *branchPrefix}, Repos: allow, Publisher: publisher, Log: logger})
	if err != nil {
		return fail(stderr, err)
	}
	mux := http.NewServeMux()
	mux.Handle(githubhooks.Path, handler)
	listener, err := net.Listen("tcp", *listen)
	if err != nil {
		return fail(stderr, err)
	}
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: time.Minute, ErrorLog: logger}
	logger.Printf("github-hooks: listening on http://%s%s, publishing %s and %s to team %s through %s; repos %s; branches %s", listener.Addr(), githubhooks.Path, *ciEvent, *mergedEvent, *team, remote.where(socketPath), strings.Join(allow, ","), branchesOf(*branchPrefix))
	served := make(chan error, 1)
	go func() { served <- server.Serve(listener) }()
	select {
	case err := <-served:
		return fail(stderr, err)
	case <-ctx.Done():
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = server.Shutdown(shutdown)
	logger.Printf("github-hooks: stopped")
	return exitOK
}

// githubHooksRedeliver asks GitHub to redeliver the failed deliveries:
// meant for the tunnel unit's ExecStartPost, so nothing waits on a timer.
func githubHooksRedeliver(ctx context.Context, api githubhooks.HookAPI, now time.Time, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("github-hooks redeliver", flag.ContinueOnError)
	flags.SetOutput(stderr)
	since := flags.Duration("since", 24*time.Hour, "how far back a failed delivery is redelivered (GitHub keeps three days)")
	repos := flags.String("repos", "", "the repositories, owner/name comma-separated (required)")
	hookURL := flags.String("hook-url", "", "the hook's URL; absent: the hooks whose URL's path is /github")
	gh := flags.String("gh", "gh", "the gh CLI, signed in as an admin of the repositories")
	positional, ok := parseAnywhere(flags, args)
	if !ok {
		return exitUsage
	}
	if len(positional) != 0 || *since <= 0 || *repos == "" {
		fmt.Fprintln(stderr, "axlr-bots: github-hooks redeliver takes --repos a/b,c/d [--since 24h] [--hook-url <url>] [--gh <path>]")
		return exitUsage
	}
	allow, err := githubhooks.ParseRepos(*repos)
	if err != nil {
		return fail(stderr, err)
	}
	if api == nil {
		api = githubhooks.GH{Bin: *gh}
	}
	redelivered, err := githubhooks.Redeliver(ctx, api, githubhooks.RedeliverOptions{Repos: allow, HookURL: *hookURL, Since: *since, Now: now})
	printJSON(stdout, redelivered)
	if err != nil {
		return fail(stderr, err)
	}
	return exitOK
}

// branchesOf is the branches a prefix maps, for the log.
func branchesOf(prefix string) string {
	if prefix == "" {
		return "every branch"
	}
	return prefix + "*"
}

// loopbackOnly refuses a listen address that is not loopback, unless
// public says so: the adapter sits behind the tunnel, and the HMAC alone
// would then be all that stands in front of the operator's token.
func loopbackOnly(addr string, public bool) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("--listen %s: %w", addr, err)
	}
	if public {
		return nil
	}
	if ip := net.ParseIP(host); host == "localhost" || (ip != nil && ip.IsLoopback()) {
		return nil
	}
	return fmt.Errorf("--listen %s is not a loopback address; give --public to listen on it", addr)
}

// readHookSecret reads the hook's secret, refusing a file others may
// read and an empty one.
func readHookSecret(path string) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("--secret-file %s is not a regular file", path)
	}
	if perm := info.Mode().Perm(); perm&0o077 != 0 {
		return nil, fmt.Errorf("--secret-file %s is readable by its group or others (%#o); chmod 600 it", path, perm)
	}
	secret, err := readSecretFile(path)
	if err != nil {
		return nil, err
	}
	if secret == "" {
		return nil, fmt.Errorf("--secret-file %s is empty", path)
	}
	return []byte(secret), nil
}
