package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/underpass-ai/axlr-bots/adapters/codex"
)

// codexProxyKeyEnv names the variable that holds the proxy's key; a
// secret never goes on the command line.
const codexProxyKeyEnv = "AXLR_CODEX_PROXY_KEY"

// codexProxy is `axlr-node codex-proxy`: the loopback Responses endpoint
// through which the node's AXLR runs reach Codex's backend on the host
// user's ChatGPT login (adapters/codex). It refuses an address off this
// host, since whoever reaches it spends the subscription, and reads the
// login once before it listens so a missing one fails the start.
func codexProxy(ctx context.Context, args []string, stderr io.Writer, listening func(net.Addr)) int {
	flags := flag.NewFlagSet("axlr-node codex-proxy", flag.ContinueOnError)
	flags.SetOutput(stderr)
	listen := flags.String("listen", envOr(codexProxyListenEnv, defaultCodexListen), "the loopback address to serve on (also AXLR_CODEX_PROXY_LISTEN)")
	codexHome := flags.String("codex-home", envOr("CODEX_HOME", defaultCodexHome()), "the Codex home whose auth.json holds the ChatGPT login (also CODEX_HOME)")
	upstream := flags.String("upstream", envOr("AXLR_CODEX_PROXY_UPSTREAM", codex.DefaultUpstream), "Codex's backend (also AXLR_CODEX_PROXY_UPSTREAM)")
	tokenURL := flags.String("token-url", envOr("AXLR_CODEX_PROXY_TOKEN_URL", codex.DefaultTokenURL), "the OAuth endpoint the login is refreshed against (also AXLR_CODEX_PROXY_TOKEN_URL)")
	refreshBefore := flags.Duration("refresh-before", envDuration("AXLR_CODEX_PROXY_REFRESH_BEFORE", codex.DefaultRefreshBefore), "refresh the login this long before its access token expires (also AXLR_CODEX_PROXY_REFRESH_BEFORE)")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return exitOK
		}
		return exitUsage
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(stderr, "axlr-node codex-proxy: takes flags only")
		return exitUsage
	}
	if err := loopbackOnly(*listen); err != nil {
		fmt.Fprintln(stderr, "axlr-node codex-proxy:", err)
		return exitUsage
	}
	logger := log.New(stderr, "", log.LstdFlags)
	auth := &codex.AuthFile{Path: filepath.Join(*codexHome, "auth.json"), TokenURL: *tokenURL, RefreshBefore: *refreshBefore}
	if _, err := auth.Credentials(ctx); err != nil {
		fmt.Fprintln(stderr, "axlr-node codex-proxy:", err)
		return exitError
	}
	listener, err := net.Listen("tcp", *listen)
	if err != nil {
		fmt.Fprintln(stderr, "axlr-node codex-proxy:", err)
		return exitError
	}
	proxy := &codex.Proxy{Auth: auth, Upstream: *upstream, Key: os.Getenv(codexProxyKeyEnv), Logf: logger.Printf}
	server := &http.Server{Handler: proxy, ReadHeaderTimeout: 10 * time.Second}
	if proxy.Key == "" {
		logger.Printf("codex-proxy: %s is not set: any process on this host may use the proxy", codexProxyKeyEnv)
	}
	logger.Printf("codex-proxy: serving /v1/responses on %s, sent to %s/responses", listener.Addr(), *upstream)
	if listening != nil {
		listening(listener.Addr())
	}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
	}()
	if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
		fmt.Fprintln(stderr, "axlr-node codex-proxy:", err)
		return exitError
	}
	return exitOK
}

// loopbackOnly refuses an address that is not this host's loopback.
func loopbackOnly(address string) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("--listen %q: %w", address, err)
	}
	if host == "localhost" {
		return nil
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		return nil
	}
	return fmt.Errorf("--listen %q is not a loopback address: the proxy spends the host user's subscription", address)
}

func defaultCodexHome() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".codex"
	}
	return filepath.Join(home, ".codex")
}
