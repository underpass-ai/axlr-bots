package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/underpass-ai/axlr-bots/adapters/pki"
)

// caDir is the fleet CA's directory under the state directory.
func caDir(stateDir string) string { return filepath.Join(stateDir, pki.CADirName) }

// caInit creates the fleet CA: the key (mode 0600) and the certificate
// under <state>/ca. It refuses to overwrite one.
func caInit(stateDir string, args []string, stdout, stderr io.Writer) int {
	if len(args) != 0 {
		fmt.Fprintln(stderr, "axlr-bots: ca init takes no arguments")
		return exitUsage
	}
	authority, err := pki.Generate(caDir(stateDir))
	if err != nil {
		return fail(stderr, err)
	}
	printJSON(stdout, caReport(authority, caDir(stateDir)))
	return exitOK
}

// caShow prints the CA's certificate details, never the key.
func caShow(stateDir string, args []string, stdout, stderr io.Writer) int {
	if len(args) != 0 {
		fmt.Fprintln(stderr, "axlr-bots: ca show takes no arguments")
		return exitUsage
	}
	authority, err := pki.Load(caDir(stateDir))
	if err != nil {
		return fail(stderr, err)
	}
	printJSON(stdout, caReport(authority, caDir(stateDir)))
	return exitOK
}

func caReport(authority pki.Authority, dir string) map[string]any {
	cert := authority.Certificate()
	return map[string]any{
		"dir":         dir,
		"subject":     cert.Subject.CommonName,
		"fingerprint": authority.Fingerprint(),
		"notAfter":    cert.NotAfter,
		"certificate": string(authority.CertificatePEM()),
	}
}

// caIssueClient writes a client certificate of the fleet CA for one of
// its own services under --out: client.key (0600), client.crt and the
// CA's ca.crt. It refuses to overwrite a key, so an init container may
// run it on every start after a test -f.
func caIssueClient(stateDir string, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("ca issue-client", flag.ContinueOnError)
	flags.SetOutput(stderr)
	name := flags.String("name", "", "the certificate's common name, such as control-plane")
	out := flags.String("out", "", "the directory to write client.key, client.crt and ca.crt in")
	lifetime := flags.Duration("lifetime", 0, "how long the certificate lives (default 365 days)")
	if err := flags.Parse(args); err != nil {
		return exitUsage
	}
	if flags.NArg() != 0 || *name == "" || *out == "" {
		fmt.Fprintln(stderr, "axlr-bots: ca issue-client needs --name and --out, and flags only")
		return exitUsage
	}
	authority, err := pki.Load(caDir(stateDir))
	if err != nil {
		return fail(stderr, err)
	}
	keyPath := filepath.Join(*out, "client.key")
	if _, err := os.Stat(keyPath); err == nil {
		return fail(stderr, errors.New(keyPath+" exists; remove it to issue another"))
	}
	keyPEM, certPEM, err := authority.IssueClient(*name, *lifetime)
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
	}{{"client.key", keyPEM, 0o600}, {"client.crt", certPEM, 0o644}, {"ca.crt", authority.CertificatePEM(), 0o644}} {
		if err := os.WriteFile(filepath.Join(*out, file.name), file.data, file.mode); err != nil {
			return fail(stderr, err)
		}
	}
	printJSON(stdout, map[string]any{"dir": *out, "subject": "CN=" + *name + ",O=" + pki.ClientOrganization, "ca": authority.Fingerprint()})
	return exitOK
}
