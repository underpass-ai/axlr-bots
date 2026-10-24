package pki

import (
	"crypto/tls"
	"sync"
	"time"
)

// reissueBefore is how close to expiry the server certificate is replaced.
const reissueBefore = 7 * 24 * time.Hour

// serverCertificate issues the control plane's certificate at the first
// handshake and again when it nears expiry, so a daemon that runs for
// years never presents an expired one.
type serverCertificate struct {
	authority Authority
	hosts     []string
	mu        sync.Mutex
	current   *tls.Certificate
}

func (s *serverCertificate) get(*tls.ClientHelloInfo) (*tls.Certificate, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.current != nil && time.Until(s.current.Leaf.NotAfter) > reissueBefore {
		return s.current, nil
	}
	issued, err := s.authority.IssueServer(s.hosts, ServerCertificateLifetime)
	if err != nil {
		return nil, err
	}
	s.current = &issued
	return s.current, nil
}

// ServerTLSConfig is how the control plane listens for nodes: TLS 1.3, a
// certificate for the hosts issued by the fleet CA, and a client
// certificate verified against that CA when one is presented. Presence
// is the routes' decision: enrollment has no certificate yet, every
// other node route requires one.
func ServerTLSConfig(authority Authority, hosts []string) *tls.Config {
	issuer := &serverCertificate{authority: authority, hosts: hosts}
	return &tls.Config{
		MinVersion:     tls.VersionTLS13,
		ClientAuth:     tls.VerifyClientCertIfGiven,
		ClientCAs:      authority.Pool(),
		GetCertificate: issuer.get,
	}
}
