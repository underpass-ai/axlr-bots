package main

import (
	"fmt"
	"io"
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
