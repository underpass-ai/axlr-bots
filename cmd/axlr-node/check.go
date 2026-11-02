package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/underpass-ai/axlr-bots/adapters/nodeagent"
	"github.com/underpass-ai/axlr-bots/adapters/pki"
	"github.com/underpass-ai/axlr-bots/nodeproto"
)

// check asks the control plane's node address for /healthz over TLS 1.3,
// verifying it against the fleet CA and nothing else, and presents no
// certificate. It changes nothing on either side: a pod's test (or an
// operator) runs it to see that the network path, the name in the
// server certificate and the CA all hold before a node enrolls.
func check(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("axlr-node check", flag.ContinueOnError)
	flags.SetOutput(stderr)
	control := flags.String("control", envOr("AXLR_NODE_CONTROL", ""), "the control plane: https://host:7878 (also AXLR_NODE_CONTROL)")
	caFile := flags.String("ca-file", envOr("AXLR_NODE_CA_FILE", ""), "the fleet CA in PEM (also AXLR_NODE_CA_FILE)")
	tokenFile := flags.String("token-file", envOr("AXLR_NODE_TOKEN_FILE", ""), "take the CA from the token in this file when no --ca-file is given (also AXLR_NODE_TOKEN_FILE)")
	stateDir := flags.String("state", envOr("AXLR_NODE_STATE", defaultStateDir()), "take the CA an enrolled node stored when neither is given")
	timeout := flags.Duration("timeout", 10*time.Second, "how long to wait for the answer")
	renewBefore := flags.Duration("renew-before", envDuration("AXLR_NODE_RENEW_BEFORE", nodeagent.RenewCertificateBefore), "say a certificate under the state is due for renewal this long before it expires, as the agent does (also AXLR_NODE_RENEW_BEFORE)")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return exitOK
		}
		return exitUsage
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(stderr, "axlr-node: check takes flags only")
		return exitUsage
	}
	if !strings.HasPrefix(*control, "https://") {
		fmt.Fprintln(stderr, "axlr-node: check needs --control https://host:port (the control plane's TLS address)")
		return exitUsage
	}
	caPEM, source, err := checkCA(*caFile, *tokenFile, *stateDir)
	if err != nil {
		fmt.Fprintln(stderr, "axlr-node: check:", err)
		return exitError
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	status, err := checkHealth(ctx, *control, caPEM)
	if err != nil {
		fmt.Fprintln(stderr, "axlr-node: check:", err)
		return exitError
	}
	answer := map[string]string{"control": *control, "status": status, "ca": source}
	for key, value := range certificateStatus(*stateDir, time.Now(), *renewBefore) {
		answer[key] = value
	}
	printJSON(stdout, answer)
	return exitOK
}

// checkCA finds the CA to verify the control plane by: the file, else
// the token's, else the one enrollment stored. The second value names
// where it came from.
func checkCA(caFile, tokenFile, stateDir string) ([]byte, string, error) {
	if caFile != "" {
		data, err := os.ReadFile(caFile)
		return data, caFile, err
	}
	if tokenFile != "" {
		raw, err := os.ReadFile(tokenFile)
		if err != nil {
			return nil, "", err
		}
		token, err := nodeproto.DecodeEnrollToken(string(raw))
		if err != nil {
			return nil, "", fmt.Errorf("%s: %w", tokenFile, err)
		}
		return []byte(token.CAPEM), "the token at " + tokenFile, nil
	}
	path := filepath.Join(stateDir, pki.CredentialsDirName, "ca.crt")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, "", fmt.Errorf("no CA: give --ca-file or --token-file, or enroll first (%w)", err)
	}
	return data, path, nil
}

// checkHealth GETs /healthz and returns the status it answers.
func checkHealth(ctx context.Context, control string, caPEM []byte) (string, error) {
	config, err := pki.EnrollTLSConfig(caPEM)
	if err != nil {
		return "", err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(control, "/")+"/healthz", nil)
	if err != nil {
		return "", err
	}
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: config}}
	response, err := client.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	var health struct {
		Status string `json:"status"`
	}
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("/healthz answered %s", response.Status)
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 4096)).Decode(&health); err != nil {
		return "", fmt.Errorf("/healthz: %w", err)
	}
	return health.Status, nil
}

// certificateStatus says, for a node enrolled under the state
// directory, its certificate's serial and expiry and whether the agent
// renews it at its next check; nothing for a node not enrolled.
func certificateStatus(stateDir string, now time.Time, before time.Duration) map[string]string {
	creds, err := pki.LoadCredentials(stateDir)
	if err != nil {
		return nil
	}
	due := "no"
	switch {
	case !creds.NotAfter.After(now):
		due = "expired: enroll again with a fresh token"
	case nodeagent.RenewalDue(creds.NotAfter, now, before):
		due = "yes"
	}
	return map[string]string{"certificate": creds.Serial, "notAfter": creds.NotAfter.UTC().Format(time.RFC3339), "renewalDue": due}
}
