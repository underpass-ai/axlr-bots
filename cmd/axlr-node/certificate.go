package main

import (
	"sync"
	"time"

	"github.com/underpass-ai/axlr-bots/adapters/pki"
	"github.com/underpass-ai/axlr-bots/adapters/unixapi"
	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

// nodeCertificate is the agent's certificate over the credentials
// enroll stored under the state directory: a renewal writes the new key
// and certificate there and makes the client present them.
type nodeCertificate struct {
	stateDir string
	client   *unixapi.Client

	mu       sync.Mutex
	notAfter time.Time
}

func newNodeCertificate(stateDir string, client *unixapi.Client, creds pki.Credentials) *nodeCertificate {
	return &nodeCertificate{stateDir: stateDir, client: client, notAfter: creds.NotAfter}
}

func (c *nodeCertificate) NotAfter() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.notAfter
}

func (c *nodeCertificate) NewRequest(node domain.NodeID) ([]byte, []byte, error) {
	return pki.NewNodeKeyAndRequest(node)
}

// Install checks the key and the certificate go together before it
// writes either, then saves them and swaps the client's TLS
// configuration. The files come first: a node that restarts after a
// renewal must find the certificate the control plane now knows.
func (c *nodeCertificate) Install(keyPEM []byte, renewed application.RenewedCertificate) error {
	creds, err := pki.ParseCredentials(keyPEM, renewed.CertificatePEM, renewed.CAPEM)
	if err != nil {
		return err
	}
	config, err := creds.ClientTLSConfig()
	if err != nil {
		return err
	}
	if err := pki.SaveCredentials(c.stateDir, keyPEM, renewed.CertificatePEM, renewed.CAPEM); err != nil {
		return err
	}
	c.client.UseTLSConfig(config)
	c.mu.Lock()
	c.notAfter = creds.NotAfter
	c.mu.Unlock()
	return nil
}
