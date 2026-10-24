package unixapi

import (
	"context"
	"net/http"

	"github.com/underpass-ai/axlr-bots/adapters/pki"
	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

// nodeIdentity is who a node request comes from when a client
// certificate authenticated it: the common name and the serial.
type nodeIdentity struct {
	ID     domain.NodeID
	Serial string
}

type identityKey struct{}

// identityOf is the certificate's identity on the request, if any.
func identityOf(r *http.Request) (nodeIdentity, bool) {
	identity, ok := r.Context().Value(identityKey{}).(nodeIdentity)
	return identity, ok
}

// authenticated wraps a node route. Over TLS the request must carry a
// client certificate (the handshake already verified it against the
// fleet CA), its common name must be the node id in the path, and the
// certificate must be the node's current one and not revoked; the
// identity is then on the context for the handler. Over the socket,
// where no certificate exists, the path is trusted as before.
func (s *Server) authenticated(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.TLS == nil {
			next(w, r)
			return
		}
		if len(r.TLS.PeerCertificates) == 0 {
			respond(w, 0, nil, domain.ForbiddenError{Reason: "a client certificate is required"})
			return
		}
		leaf := r.TLS.PeerCertificates[0]
		id, err := domain.NewNodeID(leaf.Subject.CommonName)
		if err != nil {
			respond(w, 0, nil, domain.ForbiddenError{Reason: "the certificate's common name is not a node id: " + err.Error()})
			return
		}
		if pathID := r.PathValue("id"); pathID != "" && pathID != id.String() {
			respond(w, 0, nil, domain.ForbiddenError{Reason: "the certificate is for node " + id.String() + ", the request names " + pathID})
			return
		}
		identity := nodeIdentity{ID: id, Serial: pki.SerialOf(leaf)}
		if err := application.NewAuthenticateNode(s.service.Dependencies()).Execute(r.Context(), identity.ID, identity.Serial); err != nil {
			respond(w, 0, nil, err)
			return
		}
		next(w, r.WithContext(context.WithValue(r.Context(), identityKey{}, identity)))
	}
}
