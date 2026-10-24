package application

import "time"

// IssuedCertificate is a node certificate the Certifier signed.
type IssuedCertificate struct {
	CertificatePEM []byte
	Serial         string
	NotAfter       time.Time
}
