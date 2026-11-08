// Package snstest signs Amazon SNS messages the way SNS does, with an RSA
// key and a self-signed certificate made for the test, and serves that
// certificate as a fake transport. The bodies under domain/testdata/sns-*
// are composed from AWS's documented formats, not captured: their
// signatures are placeholders, and a test re-signs them with Sign.
package snstest

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1" // #nosec G505 -- SNS SignatureVersion 1.
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math/big"
	"sync"
	"time"

	"github.com/underpass-ai/axlr-bots/domain"
)

// Signer is a key and its certificate.
type Signer struct {
	Key         *rsa.PrivateKey
	Certificate *x509.Certificate
	// PEM is the certificate as SNS serves it.
	PEM []byte
}

// NewSigner makes a 2048-bit key and a certificate valid for a day
// around now.
func NewSigner(now time.Time) (*Signer, error) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, err
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "sns.amazonaws.com"},
		NotBefore: now.Add(-12 * time.Hour), NotAfter: now.Add(12 * time.Hour), KeyUsage: x509.KeyUsageDigitalSignature,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return nil, err
	}
	certificate, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, err
	}
	block := "-----BEGIN CERTIFICATE-----\n" + base64.StdEncoding.EncodeToString(der) + "\n-----END CERTIFICATE-----\n"
	return &Signer{Key: key, Certificate: certificate, PEM: []byte(block)}, nil
}

// Sign returns the body with a Signature of its fields under the
// signer's key, with the SignatureVersion it already names, after edit
// (when given) has changed the message.
func (s *Signer) Sign(body []byte, edit func(*domain.SNSMessage)) ([]byte, error) {
	var m domain.SNSMessage
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, err
	}
	if edit != nil {
		edit(&m)
	}
	signed := []byte(m.StringToSign())
	var signature []byte
	var err error
	if m.SignatureVersion == "1" {
		sum := sha1.Sum(signed) // #nosec G401 -- SNS SignatureVersion 1.
		signature, err = rsa.SignPKCS1v15(rand.Reader, s.Key, crypto.SHA1, sum[:])
	} else {
		sum := sha256.Sum256(signed)
		signature, err = rsa.SignPKCS1v15(rand.Reader, s.Key, crypto.SHA256, sum[:])
	}
	if err != nil {
		return nil, err
	}
	m.Signature = base64.StdEncoding.EncodeToString(signature)
	return json.Marshal(m)
}

// Transport is a fake SNS transport: it serves the signer's certificate
// for any URL, and records the confirmations asked of it.
type Transport struct {
	Signer *Signer
	// Fail, when set, is what both calls answer.
	Fail error
	// ConfirmFail, when set, is what a confirmation answers.
	ConfirmFail error

	mu        sync.Mutex
	confirmed []string
	fetched   []string
}

// SigningCertificate answers the signer's certificate.
func (t *Transport) SigningCertificate(_ context.Context, url string) (*x509.Certificate, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.fetched = append(t.fetched, url)
	if t.Fail != nil {
		return nil, t.Fail
	}
	if t.Signer == nil {
		return nil, errors.New("snstest: no signer")
	}
	return t.Signer.Certificate, nil
}

// ConfirmSubscription records the URL.
func (t *Transport) ConfirmSubscription(_ context.Context, url string) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.Fail != nil {
		return t.Fail
	}
	if t.ConfirmFail != nil {
		return t.ConfirmFail
	}
	t.confirmed = append(t.confirmed, url)
	return nil
}

// Confirmed lists the subscribe URLs visited.
func (t *Transport) Confirmed() []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]string(nil), t.confirmed...)
}

// Fetched lists the certificate URLs asked for.
func (t *Transport) Fetched() []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]string(nil), t.fetched...)
}
