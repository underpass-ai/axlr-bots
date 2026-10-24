package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/underpass-ai/axlr-bots/adapters/pki"
	"github.com/underpass-ai/axlr-bots/adapters/unixapi"
	"github.com/underpass-ai/axlr-bots/domain"
	"github.com/underpass-ai/axlr-bots/nodeproto"
)

// enroll makes this host a node of the fleet: it decodes the token, trusts
// the CA the token carries (or --ca-file), generates the node's key here,
// sends a signing request with the token over TLS and stores the
// certificate, the key and the CA under the node's state. The key never
// leaves the host; the token works once.
func enroll(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("axlr-node enroll", flag.ContinueOnError)
	flags.SetOutput(stderr)
	control := flags.String("control", envOr("AXLR_NODE_CONTROL", ""), "the control plane: https://host:7878 (also AXLR_NODE_CONTROL)")
	token := flags.String("token", envOr("AXLR_NODE_TOKEN", ""), "the one-time token axlr-bots node enroll-token printed (also AXLR_NODE_TOKEN)")
	tokenFile := flags.String("token-file", envOr("AXLR_NODE_TOKEN_FILE", ""), "a file holding the token, in place of --token (also AXLR_NODE_TOKEN_FILE)")
	caFile := flags.String("ca-file", "", "the fleet CA in PEM, instead of the one the token carries")
	stateDir := flags.String("state", envOr("AXLR_NODE_STATE", defaultStateDir()), "the node's state; the credentials go under its pki/ directory")
	hostName := flags.String("host-name", "", "the host name to report (default: the OS's)")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return exitOK
		}
		return exitUsage
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(stderr, "axlr-node: enroll takes flags only")
		return exitUsage
	}
	if !strings.HasPrefix(*control, "https://") {
		fmt.Fprintln(stderr, "axlr-node: enroll needs --control https://host:port (the control plane's TLS address)")
		return exitUsage
	}
	if *token == "" && *tokenFile != "" {
		raw, err := os.ReadFile(*tokenFile)
		if err != nil {
			fmt.Fprintln(stderr, "axlr-node:", err)
			return exitError
		}
		*token = string(raw)
	}
	if *token == "" {
		fmt.Fprintln(stderr, "axlr-node: enroll needs --token or --token-file (or AXLR_NODE_TOKEN, AXLR_NODE_TOKEN_FILE)")
		return exitUsage
	}
	decoded, err := nodeproto.DecodeEnrollToken(*token)
	if err != nil {
		fmt.Fprintln(stderr, "axlr-node:", err)
		return exitUsage
	}
	caPEM := []byte(decoded.CAPEM)
	if *caFile != "" {
		if caPEM, err = os.ReadFile(*caFile); err != nil {
			fmt.Fprintln(stderr, "axlr-node:", err)
			return exitError
		}
	}
	result, err := enrollWith(context.Background(), *control, decoded, caPEM, *stateDir, *hostName)
	if err != nil {
		fmt.Fprintln(stderr, "axlr-node: enroll:", err)
		return exitError
	}
	printJSON(stdout, result)
	return exitOK
}

// enrollResult is what enroll prints.
type enrollResult struct {
	NodeID   string            `json:"nodeId"`
	Control  string            `json:"control"`
	Serial   string            `json:"serial"`
	NotAfter time.Time         `json:"notAfter"`
	Labels   map[string]string `json:"labels,omitempty"`
	State    string            `json:"state"`
}

// enrollWith does the enrollment with the pieces in hand.
func enrollWith(ctx context.Context, control string, token nodeproto.EnrollToken, caPEM []byte, stateDir, hostName string) (enrollResult, error) {
	nodeID, err := domain.NewNodeID(token.NodeID)
	if err != nil {
		return enrollResult{}, err
	}
	config, err := pki.EnrollTLSConfig(caPEM)
	if err != nil {
		return enrollResult{}, err
	}
	keyPEM, csrPEM, err := pki.NewNodeKeyAndRequest(nodeID)
	if err != nil {
		return enrollResult{}, err
	}
	if hostName == "" {
		hostName, _ = os.Hostname()
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	response, err := unixapi.NewTLSClient(control, config).Enroll(ctx, nodeproto.EnrollRequest{TokenID: token.ID, Secret: token.Secret, HostName: hostName, CSR: string(csrPEM)})
	if err != nil {
		return enrollResult{}, err
	}
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		return enrollResult{}, err
	}
	if err := pki.SaveCredentials(stateDir, keyPEM, []byte(response.Certificate), []byte(response.CA)); err != nil {
		return enrollResult{}, err
	}
	record := enrollmentRecord{NodeID: response.Node.ID, Labels: response.Node.Labels, Control: control, Serial: response.Serial, TokenID: token.ID}
	if err := record.save(stateDir); err != nil {
		return enrollResult{}, err
	}
	return enrollResult{NodeID: response.Node.ID, Control: control, Serial: response.Serial, NotAfter: response.NotAfter, Labels: response.Node.Labels, State: stateDir}, nil
}
