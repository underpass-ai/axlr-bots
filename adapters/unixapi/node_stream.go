package unixapi

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
	"github.com/underpass-ai/axlr-bots/nodeproto"
)

// stream is GET /v1/nodes/{id}/stream: held open, one JSON line per
// push. It opens with a resync (the node may have missed pushes while
// it had no stream), then says, as each change is recorded, which of the
// node's executions were cancelled, preempted, expired or given a
// message, so the node renews their leases and relays their messages
// now instead of at its next renewal. A push is a hint: the lease
// renewal and the messages route are still where the node learns what
// happened.
func (s *Server) stream(w http.ResponseWriter, r *http.Request) {
	id, err := domain.NewNodeID(r.PathValue("id"))
	if err != nil {
		respond(w, 0, nil, err)
		return
	}
	deps := s.service.Dependencies()
	if _, err := deps.Nodes.Get(r.Context(), id); err != nil {
		respond(w, 0, nil, err)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming is not supported", http.StatusInternalServerError)
		return
	}
	changed, seq := s.service.changes.next()
	w.Header().Set("Content-Type", "application/x-ndjson")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	encoder := json.NewEncoder(w)
	send := func(message nodeproto.StreamMessage) bool {
		if encoder.Encode(message) != nil {
			return false
		}
		flusher.Flush()
		return true
	}
	if !send(nodeproto.StreamMessage{Type: nodeproto.StreamResync}) {
		return
	}
	ping := time.NewTicker(nodeproto.StreamPingEvery)
	defer ping.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-s.closing:
			return
		case <-ping.C:
			if !send(nodeproto.StreamMessage{Type: nodeproto.StreamPing}) {
				return
			}
		case <-changed:
			changed, _ = s.service.changes.next()
			var pushes []application.NodePush
			var complete bool
			pushes, seq, complete = s.service.changes.since(id, seq)
			if !complete {
				if !send(nodeproto.StreamMessage{Type: nodeproto.StreamResync}) {
					return
				}
				continue
			}
			for _, push := range pushes {
				message := nodeproto.StreamMessage{Type: nodeproto.StreamExecution, ExecutionID: push.Execution.String()}
				if push.All {
					message = nodeproto.StreamMessage{Type: nodeproto.StreamResync}
				}
				if !send(message) {
					return
				}
			}
		}
	}
}
