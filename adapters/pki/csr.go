package pki

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"

	"github.com/underpass-ai/axlr-bots/domain"
)

// NewNodeKeyAndRequest generates the node's key, which never leaves the
// host, and the signing request that names the node, both in PEM.
func NewNodeKeyAndRequest(nodeID domain.NodeID) (keyPEM, csrPEM []byte, err error) {
	key, err := newKey()
	if err != nil {
		return nil, nil, err
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, nil, fmt.Errorf("pki: %w", err)
	}
	template := &x509.CertificateRequest{Subject: pkix.Name{CommonName: nodeID.String(), Organization: []string{"axlr-bots nodes"}}}
	csrDER, err := x509.CreateCertificateRequest(rand.Reader, template, key)
	if err != nil {
		return nil, nil, fmt.Errorf("pki: %w", err)
	}
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
	csrPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: csrDER})
	return keyPEM, csrPEM, nil
}

// newKey draws an ECDSA P-256 key.
func newKey() (*ecdsa.PrivateKey, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("pki: %w", err)
	}
	return key, nil
}
