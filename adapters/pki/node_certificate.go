package pki

import (
	"crypto/x509"
	"slices"
)

// IsNodeCertificate reports whether the certificate was issued to a
// node: its organisation says NodeOrganization. The fleet CA also signs
// the clients of IssueClient, with the same client-auth usage; their
// common name is free text and must never be taken for a node's id.
func IsNodeCertificate(cert *x509.Certificate) bool {
	return slices.Contains(cert.Subject.Organization, NodeOrganization)
}
