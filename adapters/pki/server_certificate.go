package pki

import (
	"crypto/tls"
	"slices"
	"sync"
	"time"
)

// reissueBefore is how close to expiry the server certificate is replaced.
const reissueBefore = 7 * 24 * time.Hour

// serverCertificate issues the control plane's certificate at the first
// handshake, again when it nears expiry, so a daemon that runs for years
// never presents an expired one, and again when the names it must carry
// change. The names are read at each handshake, not once at start: on
// 25 October 2026 the daemon started in the same second the host's LAN
// address arrived, listed only the loopback, and every node on another
// host refused its certificate until the daemon was restarted.
type serverCertificate struct {
	authority Authority
	hosts     func() []string
	mu        sync.Mutex
	current   *tls.Certificate
	issuedFor []string
}

func (s *serverCertificate) get(*tls.ClientHelloInfo) (*tls.Certificate, error) {
	hosts := normalizedHosts(s.hosts())
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.current != nil && time.Until(s.current.Leaf.NotAfter) > reissueBefore && slices.Equal(hosts, s.issuedFor) {
		return s.current, nil
	}
	issued, err := s.authority.IssueServer(hosts, ServerCertificateLifetime)
	if err != nil {
		return nil, err
	}
	s.current, s.issuedFor = &issued, hosts
	return s.current, nil
}

// normalizedHosts sorts the names and drops repeats, so the same set
// read twice compares equal and a certificate lists each name once.
func normalizedHosts(hosts []string) []string {
	out := slices.Clone(hosts)
	slices.Sort(out)
	return slices.Compact(out)
}

// ServerTLSConfig is how the control plane listens for nodes: TLS 1.3, a
// certificate for the hosts issued by the fleet CA, and a client
// certificate verified against that CA when one is presented. Presence
// is the routes' decision: enrollment has no certificate yet, every
// other node route requires one.
func ServerTLSConfig(authority Authority, hosts []string) *tls.Config {
	fixed := slices.Clone(hosts)
	return ServerTLSConfigFor(authority, func() []string { return fixed })
}

// ServerTLSConfigFor is ServerTLSConfig with the names read at each
// handshake, for a listener on every interface whose addresses can
// arrive or change after the daemon starts.
func ServerTLSConfigFor(authority Authority, hosts func() []string) *tls.Config {
	issuer := &serverCertificate{authority: authority, hosts: hosts}
	return &tls.Config{
		MinVersion:     tls.VersionTLS13,
		ClientAuth:     tls.VerifyClientCertIfGiven,
		ClientCAs:      authority.Pool(),
		GetCertificate: issuer.get,
	}
}
