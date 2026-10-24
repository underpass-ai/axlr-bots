// Package pki is the fleet's public key infrastructure: the control
// plane's certificate authority, the certificates it issues (one per
// node, with the node id as common name, and the server's own), the
// node's key and certificate signing request, and the TLS
// configurations both ends use. Nothing here knows a bot or a task; the
// application layer sees it through the Certifier port.
package pki

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"time"
)

// Files of the authority under <state>/ca.
const (
	CADirName  = "ca"
	caKeyFile  = "ca.key"
	caCertFile = "ca.crt"
	// CALifetime is how long the fleet CA is valid.
	CALifetime = 10 * 365 * 24 * time.Hour
)

// Authority is the fleet CA: its certificate and its private key.
type Authority struct {
	cert    *x509.Certificate
	key     *ecdsa.PrivateKey
	certPEM []byte
}

// Generate creates the CA under dir: ca.key (mode 0600) and ca.crt. It
// refuses to overwrite an existing one.
func Generate(dir string) (Authority, error) {
	keyPath, certPath := filepath.Join(dir, caKeyFile), filepath.Join(dir, caCertFile)
	for _, path := range []string{keyPath, certPath} {
		if _, err := os.Stat(path); err == nil {
			return Authority{}, fmt.Errorf("pki: %s exists; the fleet CA is already initialised", path)
		}
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return Authority{}, fmt.Errorf("pki: %w", err)
	}
	serial, err := newSerial()
	if err != nil {
		return Authority{}, err
	}
	now := time.Now()
	template := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "axlr-bots fleet CA", Organization: []string{"axlr-bots"}},
		NotBefore:             now.Add(-5 * time.Minute),
		NotAfter:              now.Add(CALifetime),
		IsCA:                  true,
		BasicConstraintsValid: true,
		MaxPathLenZero:        true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign | x509.KeyUsageDigitalSignature,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return Authority{}, fmt.Errorf("pki: %w", err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return Authority{}, fmt.Errorf("pki: %w", err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return Authority{}, fmt.Errorf("pki: %w", err)
	}
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}), 0o600); err != nil {
		return Authority{}, fmt.Errorf("pki: %w", err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	if err := os.WriteFile(certPath, certPEM, 0o644); err != nil {
		return Authority{}, fmt.Errorf("pki: %w", err)
	}
	return Load(dir)
}

// Load reads the CA from dir.
func Load(dir string) (Authority, error) {
	keyPEM, err := os.ReadFile(filepath.Join(dir, caKeyFile))
	if err != nil {
		return Authority{}, fmt.Errorf("pki: the fleet CA is not initialised (run axlr-bots ca init): %w", err)
	}
	certPEM, err := os.ReadFile(filepath.Join(dir, caCertFile))
	if err != nil {
		return Authority{}, fmt.Errorf("pki: %w", err)
	}
	return Parse(certPEM, keyPEM)
}

// Parse builds the authority from its certificate and key in PEM.
func Parse(certPEM, keyPEM []byte) (Authority, error) {
	cert, err := parseCertificate(certPEM)
	if err != nil {
		return Authority{}, fmt.Errorf("pki: CA certificate: %w", err)
	}
	block, _ := pem.Decode(keyPEM)
	if block == nil {
		return Authority{}, errors.New("pki: CA key: not PEM")
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return Authority{}, fmt.Errorf("pki: CA key: %w", err)
	}
	key, ok := parsed.(*ecdsa.PrivateKey)
	if !ok {
		return Authority{}, errors.New("pki: CA key: not an ECDSA key")
	}
	if !cert.IsCA {
		return Authority{}, errors.New("pki: the certificate is not a CA")
	}
	return Authority{cert: cert, key: key, certPEM: certPEM}, nil
}

// CertificatePEM is the CA certificate, what nodes trust.
func (a Authority) CertificatePEM() []byte { return append([]byte(nil), a.certPEM...) }

// Certificate is the parsed CA certificate.
func (a Authority) Certificate() *x509.Certificate { return a.cert }

// Fingerprint is the SHA-256 of the CA certificate, in hex.
func (a Authority) Fingerprint() string {
	sum := sha256.Sum256(a.cert.Raw)
	return hex.EncodeToString(sum[:])
}

// Pool is a certificate pool holding only this CA.
func (a Authority) Pool() *x509.CertPool {
	pool := x509.NewCertPool()
	pool.AddCert(a.cert)
	return pool
}

// newSerial draws a random 128-bit certificate serial.
func newSerial() (*big.Int, error) {
	limit := new(big.Int).Lsh(big.NewInt(1), 128)
	serial, err := rand.Int(rand.Reader, limit)
	if err != nil {
		return nil, fmt.Errorf("pki: %w", err)
	}
	return serial, nil
}

// parseCertificate decodes one PEM certificate.
func parseCertificate(certPEM []byte) (*x509.Certificate, error) {
	block, _ := pem.Decode(certPEM)
	if block == nil || block.Type != "CERTIFICATE" {
		return nil, errors.New("not a PEM certificate")
	}
	return x509.ParseCertificate(block.Bytes)
}
