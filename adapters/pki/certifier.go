package pki

import (
	"context"
	"path/filepath"

	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

// Certifier is the application's Certifier over the CA under
// <state>/ca. The CA is read on each call: it is two small files, and a
// `ca init` run after the daemon started is seen without a restart.
type Certifier struct {
	dir string
}

// NewCertifier signs with the CA under the state directory.
func NewCertifier(stateDir string) Certifier {
	return Certifier{dir: filepath.Join(stateDir, CADirName)}
}

// Dir is the CA's directory.
func (c Certifier) Dir() string { return c.dir }

// IssueNodeCertificate signs the node's request for NodeCertificateLifetime.
func (c Certifier) IssueNodeCertificate(_ context.Context, node domain.NodeID, csrPEM []byte) (application.IssuedCertificate, error) {
	authority, err := Load(c.dir)
	if err != nil {
		return application.IssuedCertificate{}, err
	}
	issued, err := authority.IssueNode(csrPEM, node, NodeCertificateLifetime)
	if err != nil {
		return application.IssuedCertificate{}, err
	}
	return application.IssuedCertificate{CertificatePEM: issued.CertificatePEM, Serial: issued.Serial, NotAfter: issued.NotAfter}, nil
}

// CACertificate is the CA certificate in PEM.
func (c Certifier) CACertificate(context.Context) ([]byte, error) {
	authority, err := Load(c.dir)
	if err != nil {
		return nil, err
	}
	return authority.CertificatePEM(), nil
}
