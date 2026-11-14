package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/underpass-ai/axlr-bots/adapters/filestore"
	"github.com/underpass-ai/axlr-bots/adapters/pki"
)

// caOperator is `ca operator issue|revoke`: the certificates that let a
// person's CLI or panel use the gRPC operator address.
func caOperator(ctx context.Context, stateDir string, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "axlr-bots: ca operator needs issue or revoke")
		return exitUsage
	}
	switch args[0] {
	case "issue":
		return caOperatorIssue(stateDir, args[1:], stdout, stderr)
	case "revoke":
		return caOperatorRevoke(ctx, stateDir, args[1:], stdout, stderr)
	}
	fmt.Fprintf(stderr, "axlr-bots: ca operator takes issue or revoke, not %q\n", args[0])
	return exitUsage
}

// caOperatorIssue writes operator.key (0600), operator.crt and the CA's
// ca.crt under --out (default: where the CLI and the panel look). It
// refuses to overwrite a key.
func caOperatorIssue(stateDir string, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("ca operator issue", flag.ContinueOnError)
	flags.SetOutput(stderr)
	out := flags.String("out", defaultOperatorDir(), "the directory to write operator.key, operator.crt and ca.crt in")
	lifetime := flags.Duration("lifetime", 0, "how long the certificate lives (default 90 days)")
	positional, ok := parseAnywhere(flags, args)
	if !ok {
		return exitUsage
	}
	if len(positional) != 1 {
		fmt.Fprintln(stderr, "axlr-bots: ca operator issue needs exactly one <name>")
		return exitUsage
	}
	name := positional[0]
	if err := pki.ValidateOperatorName(name); err != nil {
		fmt.Fprintln(stderr, "axlr-bots: ca operator issue:", err)
		return exitUsage
	}
	authority, err := pki.Load(caDir(stateDir))
	if err != nil {
		return fail(stderr, err)
	}
	keyPath := filepath.Join(*out, operatorKeyFile)
	if _, err := os.Stat(keyPath); err == nil {
		return fail(stderr, errors.New(keyPath+" exists; remove it to issue another"))
	}
	keyPEM, certPEM, issued, err := authority.IssueOperator(name, *lifetime)
	if err != nil {
		return fail(stderr, err)
	}
	if err := os.MkdirAll(*out, 0o700); err != nil {
		return fail(stderr, err)
	}
	for _, file := range []struct {
		name string
		data []byte
		mode os.FileMode
	}{{operatorKeyFile, keyPEM, 0o600}, {operatorCertFile, certPEM, 0o644}, {operatorCAFile, authority.CertificatePEM(), 0o644}} {
		path := filepath.Join(*out, file.name)
		if err := os.WriteFile(path, file.data, file.mode); err != nil {
			return fail(stderr, err)
		}
		// WriteFile keeps the mode of a file that was already there.
		if err := os.Chmod(path, file.mode); err != nil {
			return fail(stderr, err)
		}
	}
	printJSON(stdout, map[string]any{
		"dir": *out, "name": name, "serial": issued.Serial, "notAfter": issued.NotAfter,
		"subject": "CN=" + name + ",O=" + pki.OperatorOrganization, "ca": authority.Fingerprint(),
	})
	return exitOK
}

// caOperatorRevoke puts an operator certificate's serial on the denylist
// the nodes' revocations use; the daemon reads it at each call, so the
// operator is refused from the next request on.
func caOperatorRevoke(ctx context.Context, stateDir string, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("ca operator revoke", flag.ContinueOnError)
	flags.SetOutput(stderr)
	certFile := flags.String("cert-file", "", "the operator certificate to revoke (operator.crt)")
	serial := flags.String("serial", "", "the serial to revoke, as ca operator issue printed it")
	name := flags.String("name", "", "with --serial, the operator's name, for the record")
	positional, ok := parseAnywhere(flags, args)
	if !ok {
		return exitUsage
	}
	if len(positional) != 0 || (*certFile == "") == (*serial == "") {
		fmt.Fprintln(stderr, "axlr-bots: ca operator revoke needs --cert-file or --serial (--name), flags only")
		return exitUsage
	}
	who, revoked := *name, strings.ToLower(strings.TrimSpace(*serial))
	if *certFile != "" {
		raw, err := os.ReadFile(*certFile)
		if err != nil {
			return fail(stderr, err)
		}
		if who, revoked, err = pki.ParseOperatorCertificate(raw); err != nil {
			return fail(stderr, err)
		}
	}
	if err := filestore.NewCertificateDenylist(stateDir).Deny(ctx, revoked, "operator:"+who); err != nil {
		return fail(stderr, err)
	}
	printJSON(stdout, map[string]any{"revoked": true, "name": who, "serial": revoked})
	return exitOK
}
