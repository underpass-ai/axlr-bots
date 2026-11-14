package pki

import (
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/underpass-ai/axlr-bots/domain"
)

// What marks a certificate as an operator's. Three things must agree, and
// no certificate the authority issues for a node (IssueNode: organisation
// NodeOrganization, no URI) or for a service (IssueClient: organisation
// ClientOrganization) carries any of them: the organisation, a URI
// subject alternative name `axlr-bots:operator:<name>`, and a common name
// equal to <name>.
const (
	OperatorOrganization = "axlr-bots operators"
	// OperatorURIPrefix starts the URI SAN of an operator certificate.
	OperatorURIPrefix = "axlr-bots:operator:"
	// OperatorCertificateLifetime is how long an operator certificate
	// lives by default: shorter than a node's, as an operator's key sits
	// on a person's machine.
	OperatorCertificateLifetime = 90 * 24 * time.Hour
)

var operatorNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,62}$`)

// ValidateOperatorName checks the name an operator certificate may carry:
// letters, digits, dot, dash and underscore, starting with a letter or a
// digit. It is what the control plane records as who decided.
func ValidateOperatorName(name string) error {
	if !operatorNamePattern.MatchString(name) {
		return domain.ValidationError{Field: "name", Reason: "must be 1 to 63 letters, digits, '.', '-' or '_', starting with a letter or digit"}
	}
	return nil
}

// OperatorURI is the URI SAN of the operator's certificate.
func OperatorURI(name string) *url.URL {
	return &url.URL{Scheme: "axlr-bots", Opaque: "operator:" + name}
}

// IssueOperator signs a client certificate for an operator, with a fresh
// key, for the operator gRPC address. It returns the key and the
// certificate in PEM and what was issued.
func (a Authority) IssueOperator(name string, lifetime time.Duration) (keyPEM, certPEM []byte, issued Issued, err error) {
	if err := ValidateOperatorName(name); err != nil {
		return nil, nil, Issued{}, err
	}
	key, err := newKey()
	if err != nil {
		return nil, nil, Issued{}, err
	}
	serial, err := newSerial()
	if err != nil {
		return nil, nil, Issued{}, err
	}
	if lifetime <= 0 {
		lifetime = OperatorCertificateLifetime
	}
	now := time.Now().UTC().Truncate(time.Second)
	template := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: name, Organization: []string{OperatorOrganization}},
		URIs:                  []*url.URL{OperatorURI(name)},
		NotBefore:             now.Add(-5 * time.Minute),
		NotAfter:              now.Add(lifetime),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, a.cert, &key.PublicKey, a.key)
	if err != nil {
		return nil, nil, Issued{}, fmt.Errorf("pki: sign: %w", err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, nil, Issued{}, fmt.Errorf("pki: %w", err)
	}
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	return keyPEM, certPEM, Issued{CertificatePEM: certPEM, Serial: serial.Text(16), NotAfter: template.NotAfter}, nil
}

// OperatorName is the operator a certificate was issued to, and false for
// any other certificate: a node's, a service's, or one that only looks
// like an operator's in one of the three marks.
func OperatorName(cert *x509.Certificate) (string, bool) {
	if !slices.Contains(cert.Subject.Organization, OperatorOrganization) {
		return "", false
	}
	name := cert.Subject.CommonName
	if ValidateOperatorName(name) != nil || len(cert.URIs) != 1 {
		return "", false
	}
	if rest, ok := strings.CutPrefix(cert.URIs[0].String(), OperatorURIPrefix); !ok || rest != name {
		return "", false
	}
	if !slices.Contains(cert.ExtKeyUsage, x509.ExtKeyUsageClientAuth) {
		return "", false
	}
	return name, true
}

// ParseOperatorCertificate reads a PEM operator certificate and returns
// its name and serial; a certificate that is not an operator's is refused.
func ParseOperatorCertificate(certPEM []byte) (name, serial string, err error) {
	cert, err := parseCertificate(certPEM)
	if err != nil {
		return "", "", fmt.Errorf("pki: operator certificate: %w", err)
	}
	name, ok := OperatorName(cert)
	if !ok {
		return "", "", domain.ValidationError{Field: "certificate", Reason: "was not issued to an operator"}
	}
	return name, SerialOf(cert), nil
}
