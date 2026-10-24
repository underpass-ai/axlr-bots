package pki

import (
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"net"
	"time"

	"github.com/underpass-ai/axlr-bots/domain"
)

// Lifetimes of the certificates the authority issues.
const (
	NodeCertificateLifetime   = 365 * 24 * time.Hour
	ServerCertificateLifetime = 365 * 24 * time.Hour
)

// Issued is a certificate the authority signed.
type Issued struct {
	CertificatePEM []byte
	Serial         string
	NotAfter       time.Time
}

// IssueNode signs a node's certificate signing request. The subject is
// the authority's to set: the common name is the node id, whatever the
// request says, and a request whose common name names another node is
// refused. The key stays on the node; only its public half travels.
func (a Authority) IssueNode(csrPEM []byte, nodeID domain.NodeID, lifetime time.Duration) (Issued, error) {
	block, _ := pem.Decode(csrPEM)
	if block == nil || block.Type != "CERTIFICATE REQUEST" {
		return Issued{}, domain.ValidationError{Field: "csr", Reason: "is not a PEM certificate request"}
	}
	csr, err := x509.ParseCertificateRequest(block.Bytes)
	if err != nil {
		return Issued{}, domain.ValidationError{Field: "csr", Reason: err.Error()}
	}
	if err := csr.CheckSignature(); err != nil {
		return Issued{}, domain.ValidationError{Field: "csr", Reason: "signature: " + err.Error()}
	}
	if csr.Subject.CommonName != nodeID.String() {
		return Issued{}, domain.ValidationError{Field: "csr", Reason: fmt.Sprintf("names %q, the token is for %q", csr.Subject.CommonName, nodeID)}
	}
	if lifetime <= 0 {
		lifetime = NodeCertificateLifetime
	}
	serial, err := newSerial()
	if err != nil {
		return Issued{}, err
	}
	now := time.Now().UTC().Truncate(time.Second)
	template := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: nodeID.String(), Organization: []string{"axlr-bots nodes"}},
		NotBefore:             now.Add(-5 * time.Minute),
		NotAfter:              now.Add(lifetime),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, a.cert, csr.PublicKey, a.key)
	if err != nil {
		return Issued{}, fmt.Errorf("pki: sign: %w", err)
	}
	return Issued{CertificatePEM: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), Serial: template.SerialNumber.Text(16), NotAfter: template.NotAfter}, nil
}

// IssueServer signs the control plane's own certificate for the names
// and addresses nodes dial, with a fresh key. The chain carries the CA
// too.
func (a Authority) IssueServer(hosts []string, lifetime time.Duration) (tls.Certificate, error) {
	key, err := newKey()
	if err != nil {
		return tls.Certificate{}, err
	}
	serial, err := newSerial()
	if err != nil {
		return tls.Certificate{}, err
	}
	if lifetime <= 0 {
		lifetime = ServerCertificateLifetime
	}
	now := time.Now()
	template := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "axlr-bots control plane", Organization: []string{"axlr-bots"}},
		NotBefore:             now.Add(-5 * time.Minute),
		NotAfter:              now.Add(lifetime),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}
	for _, host := range hosts {
		if ip := net.ParseIP(host); ip != nil {
			template.IPAddresses = append(template.IPAddresses, ip)
		} else if host != "" {
			template.DNSNames = append(template.DNSNames, host)
		}
	}
	der, err := x509.CreateCertificate(rand.Reader, template, a.cert, &key.PublicKey, a.key)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("pki: sign: %w", err)
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("pki: %w", err)
	}
	return tls.Certificate{Certificate: [][]byte{der, a.cert.Raw}, PrivateKey: key, Leaf: leaf}, nil
}
