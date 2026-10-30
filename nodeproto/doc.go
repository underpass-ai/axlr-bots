// Package nodeproto holds the messages of the node protocol, shared by
// the control plane's server and the node agent's client: what a node
// says when it registers, what it is assigned, how it renews a lease and
// how it reports. The messages are plain JSON; the transport (a Unix
// socket, loopback, later TLS with client certificates) is not theirs.
//
//	node ──POST /v1/enroll─────────────────────▶ control plane   once: a token and a CSR for a certificate
//	node ──POST /v1/nodes──────────────────────▶ control plane   register (idempotent per node id)
//	node ──POST /v1/nodes/{id}/heartbeat───────▶ control plane   liveness
//	node ──GET  /v1/nodes/{id}/assignments/next?wait=20s──▶     long poll: one Assignment or 204
//	node ──GET  /v1/nodes/{id}/stream──────────▶ control plane   held open: one JSON line per push (an execution to renew now, a ping)
//	node ──POST /v1/nodes/{id}/executions/{exec}/lease─▶        renew (idempotent)
//	node ──PUT  /v1/nodes/{id}/executions/{exec}/artifacts/{name}▶ an artifact's bytes, before the report
//	node ──POST /v1/nodes/{id}/executions/{exec}/report▶        result (idempotent: accepted once)
package nodeproto
