package pki

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Files of a node's credentials under <node-state>/pki.
const (
	CredentialsDirName = "pki"
	nodeKeyFile        = "node.key"
	nodeCertFile       = "node.crt"
	nodeCAFile         = "ca.crt"
)

// Credentials are what an enrolled node holds: its key and certificate,
// and the CA it trusts the control plane by.
type Credentials struct {
	Certificate tls.Certificate
	CAPEM       []byte
	// NodeID is the certificate's common name: the node's identity.
	NodeID   string
	Serial   string
	NotAfter time.Time
}

// SaveCredentials writes the node's key (mode 0600), certificate and CA
// under dir/pki.
func SaveCredentials(dir string, keyPEM, certPEM, caPEM []byte) error {
	target := filepath.Join(dir, CredentialsDirName)
	if err := os.MkdirAll(target, 0o700); err != nil {
		return fmt.Errorf("pki: %w", err)
	}
	for _, file := range []struct {
		name string
		data []byte
		mode os.FileMode
	}{{nodeKeyFile, keyPEM, 0o600}, {nodeCertFile, certPEM, 0o644}, {nodeCAFile, caPEM, 0o644}} {
		if err := os.WriteFile(filepath.Join(target, file.name), file.data, file.mode); err != nil {
			return fmt.Errorf("pki: %w", err)
		}
	}
	return nil
}

// LoadCredentials reads what SaveCredentials wrote. A missing key is the
// sign the node was never enrolled.
func LoadCredentials(dir string) (Credentials, error) {
	target := filepath.Join(dir, CredentialsDirName)
	certPEM, err := os.ReadFile(filepath.Join(target, nodeCertFile))
	if errors.Is(err, os.ErrNotExist) {
		return Credentials{}, fmt.Errorf("pki: no node certificate under %s (run axlr-node enroll first)", target)
	}
	if err != nil {
		return Credentials{}, fmt.Errorf("pki: %w", err)
	}
	keyPEM, err := os.ReadFile(filepath.Join(target, nodeKeyFile))
	if err != nil {
		return Credentials{}, fmt.Errorf("pki: %w", err)
	}
	caPEM, err := os.ReadFile(filepath.Join(target, nodeCAFile))
	if err != nil {
		return Credentials{}, fmt.Errorf("pki: %w", err)
	}
	return ParseCredentials(keyPEM, certPEM, caPEM)
}

// ParseCredentials builds the credentials from their PEM files.
func ParseCredentials(keyPEM, certPEM, caPEM []byte) (Credentials, error) {
	certificate, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return Credentials{}, fmt.Errorf("pki: node certificate: %w", err)
	}
	leaf, err := x509.ParseCertificate(certificate.Certificate[0])
	if err != nil {
		return Credentials{}, fmt.Errorf("pki: node certificate: %w", err)
	}
	certificate.Leaf = leaf
	if _, err := parseCertificate(caPEM); err != nil {
		return Credentials{}, fmt.Errorf("pki: CA certificate: %w", err)
	}
	return Credentials{Certificate: certificate, CAPEM: caPEM, NodeID: leaf.Subject.CommonName, Serial: SerialOf(leaf), NotAfter: leaf.NotAfter}, nil
}

// ClientTLSConfig is how the node speaks to the control plane: TLS 1.3,
// its certificate presented, the control plane verified against the
// fleet CA and nothing else.
func (c Credentials) ClientTLSConfig() (*tls.Config, error) {
	pool, err := poolOf(c.CAPEM)
	if err != nil {
		return nil, err
	}
	return &tls.Config{MinVersion: tls.VersionTLS13, RootCAs: pool, Certificates: []tls.Certificate{c.Certificate}}, nil
}

// EnrollTLSConfig is how a node not yet enrolled speaks to the control
// plane: no client certificate, the server verified against the CA the
// token (or --ca-file) carried.
func EnrollTLSConfig(caPEM []byte) (*tls.Config, error) {
	pool, err := poolOf(caPEM)
	if err != nil {
		return nil, err
	}
	return &tls.Config{MinVersion: tls.VersionTLS13, RootCAs: pool}, nil
}

// poolOf is a pool holding the PEM certificates given, and only them.
func poolOf(caPEM []byte) (*x509.CertPool, error) {
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caPEM) {
		return nil, errors.New("pki: the CA bundle holds no certificate")
	}
	return pool, nil
}
