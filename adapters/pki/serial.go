package pki

import "crypto/x509"

// SerialOf is a certificate's serial as the control plane records it:
// lowercase hex, the same on the node's record and in the denylist.
func SerialOf(cert *x509.Certificate) string {
	return cert.SerialNumber.Text(16)
}
