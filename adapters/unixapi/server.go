package unixapi

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
	"github.com/underpass-ai/axlr-bots/nodeproto"
)

// maxBodyBytes bounds a request body; a bot definition or an objective
// is far smaller.
const maxBodyBytes = 1 << 20

// Server serves the operator's Service and the node protocol over HTTP:
// everything on a Unix socket (the same host), and the node protocol
// alone on a TLS address (nodes on other hosts), where a client
// certificate issued by the fleet CA names the node and an enrollment
// token is the one way in without one.
type Server struct {
	service *Local
}

// NewServer serves the local service.
func NewServer(service *Local) *Server {
	return &Server{service: service}
}

// pollInterval is how often a long poll looks again for an assignment.
const pollInterval = 250 * time.Millisecond

// maxPollWait bounds a long poll.
const maxPollWait = 30 * time.Second

// Handler is the API's routes, all of them: what the socket serves.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	s.nodeRoutes(mux)
	mux.HandleFunc("POST /v1/bots", s.registerBot)
	mux.HandleFunc("GET /v1/bots", s.listBots)
	mux.HandleFunc("GET /v1/bots/{id}", s.getBot)
	mux.HandleFunc("POST /v1/bots/{id}/enable", s.enableBot)
	mux.HandleFunc("POST /v1/tasks", s.submitTask)
	mux.HandleFunc("GET /v1/tasks/{id}", s.getTask)
	mux.HandleFunc("POST /v1/tasks/{id}/run", s.runTask)
	mux.HandleFunc("POST /v1/tasks/{id}/decide", s.decideTask)
	mux.HandleFunc("GET /v1/tasks/{id}/report", s.getReport)
	mux.HandleFunc("GET /v1/executions/{id}", s.getExecution)
	mux.HandleFunc("GET /v1/nodes", s.listNodes)
	mux.HandleFunc("GET /v1/nodes/{id}", s.getNode)
	mux.HandleFunc("POST /v1/enrollments", s.createEnrollmentToken)
	mux.HandleFunc("POST /v1/nodes/{id}/revoke", s.revokeNode)
	return mux
}

// NodeHandler is the node protocol alone: what the TLS address serves.
// A node certificate opens no operator route.
func (s *Server) NodeHandler() http.Handler {
	mux := http.NewServeMux()
	s.nodeRoutes(mux)
	return mux
}

// nodeRoutes are the routes a node uses. Enrollment is the one route a
// node reaches without a certificate; the others carry the node's
// identity in the certificate over TLS, or in the path over the socket.
func (s *Server) nodeRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("POST /v1/enroll", s.enroll)
	mux.HandleFunc("POST /v1/nodes", s.authenticated(s.registerNode))
	mux.HandleFunc("POST /v1/nodes/{id}/heartbeat", s.authenticated(s.heartbeat))
	mux.HandleFunc("GET /v1/nodes/{id}/assignments/next", s.authenticated(s.nextAssignment))
	mux.HandleFunc("POST /v1/nodes/{id}/executions/{exec}/lease", s.authenticated(s.renewLease))
	mux.HandleFunc("POST /v1/nodes/{id}/executions/{exec}/report", s.authenticated(s.report))
}

// ServeTLS serves the node protocol on the listener with the TLS
// configuration (pki.ServerTLSConfig) until ctx ends. The caller binds
// the listener, so it knows the address when it asked for port 0.
func (s *Server) ServeTLS(ctx context.Context, listener net.Listener, config *tls.Config) error {
	server := &http.Server{Handler: s.NodeHandler(), TLSConfig: config, ReadHeaderTimeout: 10 * time.Second, ErrorLog: log.New(io.Discard, "", 0)}
	return s.run(ctx, server, func() error { return server.ServeTLS(listener, "", "") }, func() {})
}

// Listen binds a TCP address for ServeTLS.
func Listen(address string) (net.Listener, error) {
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return nil, fmt.Errorf("unixapi: %w", err)
	}
	return listener, nil
}

// Serve listens on the socket until ctx ends. A socket file left by a
// dead daemon is removed first; the file is 0660 so the service's user
// and group reach it and nobody else.
func (s *Server) Serve(ctx context.Context, socketPath string) error {
	if err := os.MkdirAll(filepath.Dir(socketPath), 0o755); err != nil {
		return fmt.Errorf("unixapi: %w", err)
	}
	if err := os.Remove(socketPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("unixapi: stale socket: %w", err)
	}
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		return fmt.Errorf("unixapi: %w", err)
	}
	if err := os.Chmod(socketPath, 0o660); err != nil {
		_ = listener.Close()
		return fmt.Errorf("unixapi: %w", err)
	}
	return s.serve(ctx, listener, func() { _ = os.Remove(socketPath) })
}

// serve runs the HTTP server on the listener until ctx ends, then
// cleans up.
func (s *Server) serve(ctx context.Context, listener net.Listener, cleanup func()) error {
	server := &http.Server{Handler: s.Handler(), ReadHeaderTimeout: 10 * time.Second}
	return s.run(ctx, server, func() error { return server.Serve(listener) }, cleanup)
}

// run drives one server until ctx ends or it stops on its own.
func (s *Server) run(ctx context.Context, server *http.Server, start func() error, cleanup func()) error {
	done := make(chan error, 1)
	go func() { done <- start() }()
	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
		<-done
		cleanup()
		return nil
	case err := <-done:
		cleanup()
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("unixapi: %w", err)
	}
}

func (s *Server) registerBot(w http.ResponseWriter, r *http.Request) {
	var definition botDefinition
	if !readJSON(w, r, &definition) {
		return
	}
	value, err := s.service.RegisterBot(r.Context(), definition.BotDefinition)
	respond(w, http.StatusCreated, value, err)
}

func (s *Server) listBots(w http.ResponseWriter, r *http.Request) {
	value, err := s.service.ListBots(r.Context())
	respond(w, http.StatusOK, value, err)
}

func (s *Server) getBot(w http.ResponseWriter, r *http.Request) {
	value, err := s.service.GetBot(r.Context(), r.PathValue("id"))
	respond(w, http.StatusOK, value, err)
}

func (s *Server) enableBot(w http.ResponseWriter, r *http.Request) {
	value, err := s.service.EnableBot(r.Context(), r.PathValue("id"))
	respond(w, http.StatusOK, value, err)
}

func (s *Server) submitTask(w http.ResponseWriter, r *http.Request) {
	var request SubmitRequest
	if !readJSON(w, r, &request) {
		return
	}
	response, err := s.service.SubmitTask(r.Context(), request)
	respond(w, createdStatus(response.Created), response, err)
}

func (s *Server) getTask(w http.ResponseWriter, r *http.Request) {
	value, err := s.service.GetTask(r.Context(), r.PathValue("id"))
	respond(w, http.StatusOK, value, err)
}

func (s *Server) runTask(w http.ResponseWriter, r *http.Request) {
	value, err := s.service.RunTask(r.Context(), r.PathValue("id"))
	respond(w, http.StatusOK, value, err)
}

func (s *Server) decideTask(w http.ResponseWriter, r *http.Request) {
	var request DecideRequest
	if !readJSON(w, r, &request) {
		return
	}
	value, err := s.service.DecideTask(r.Context(), r.PathValue("id"), request.Decision)
	respond(w, http.StatusOK, value, err)
}

func (s *Server) getReport(w http.ResponseWriter, r *http.Request) {
	value, err := s.service.GetReport(r.Context(), r.PathValue("id"))
	respond(w, http.StatusOK, value, err)
}

func (s *Server) getExecution(w http.ResponseWriter, r *http.Request) {
	value, err := s.service.GetExecution(r.Context(), r.PathValue("id"))
	respond(w, http.StatusOK, value, err)
}

func (s *Server) listNodes(w http.ResponseWriter, r *http.Request) {
	value, err := s.service.ListNodes(r.Context())
	respond(w, http.StatusOK, value, err)
}

func (s *Server) getNode(w http.ResponseWriter, r *http.Request) {
	value, err := s.service.GetNode(r.Context(), r.PathValue("id"))
	respond(w, http.StatusOK, value, err)
}

func (s *Server) registerNode(w http.ResponseWriter, r *http.Request) {
	var request nodeproto.RegisterRequest
	if !readJSON(w, r, &request) {
		return
	}
	if identity, ok := identityOf(r); ok {
		if request.ID == "" {
			request.ID = identity.ID.String()
		}
		if request.ID != identity.ID.String() {
			respond(w, 0, nil, domain.ForbiddenError{Reason: "the certificate is for node " + identity.ID.String() + ", the registration names " + request.ID})
			return
		}
	}
	node, err := application.NewRegisterNode(s.service.Dependencies()).Execute(r.Context(), application.NodeRegistration{ID: request.ID, HostName: request.HostName, Labels: request.Labels, Capabilities: request.Capabilities})
	if err != nil {
		respond(w, 0, nil, err)
		return
	}
	respond(w, http.StatusOK, node.Snapshot(), nil)
}

func (s *Server) heartbeat(w http.ResponseWriter, r *http.Request) {
	id, err := domain.NewNodeID(r.PathValue("id"))
	if err != nil {
		respond(w, 0, nil, err)
		return
	}
	node, err := application.NewHeartbeatNode(s.service.Dependencies()).Execute(r.Context(), id)
	if err != nil {
		respond(w, 0, nil, err)
		return
	}
	respond(w, http.StatusOK, node.Snapshot(), nil)
}

// nextAssignment long-polls: it looks for an assignment every
// pollInterval until one appears or wait passes, then answers 204.
func (s *Server) nextAssignment(w http.ResponseWriter, r *http.Request) {
	id, err := domain.NewNodeID(r.PathValue("id"))
	if err != nil {
		respond(w, 0, nil, err)
		return
	}
	wait := time.Duration(0)
	if raw := r.URL.Query().Get("wait"); raw != "" {
		parsed, err := time.ParseDuration(raw)
		if err != nil || parsed < 0 {
			respond(w, 0, nil, domain.ValidationError{Field: "wait", Reason: "must be a duration"})
			return
		}
		wait = min(parsed, maxPollWait)
	}
	deadline := time.Now().Add(wait)
	for {
		assignment, found, err := s.service.Runner().NextAssignment(r.Context(), id)
		if err != nil {
			respond(w, 0, nil, err)
			return
		}
		if found {
			respond(w, http.StatusOK, nodeproto.FromAssignment(assignment), nil)
			return
		}
		if time.Now().After(deadline) || r.Context().Err() != nil {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		select {
		case <-r.Context().Done():
			w.WriteHeader(http.StatusNoContent)
			return
		case <-time.After(pollInterval):
		}
	}
}

func (s *Server) renewLease(w http.ResponseWriter, r *http.Request) {
	id, err := domain.NewNodeID(r.PathValue("id"))
	if err != nil {
		respond(w, 0, nil, err)
		return
	}
	executionID, err := domain.NewExecutionID(r.PathValue("exec"))
	if err != nil {
		respond(w, 0, nil, err)
		return
	}
	until, err := application.NewRenewLease(s.service.Dependencies()).Execute(r.Context(), id, executionID)
	if err != nil {
		respond(w, 0, nil, err)
		return
	}
	respond(w, http.StatusOK, nodeproto.LeaseMessage{LeaseUntil: until}, nil)
}

func (s *Server) report(w http.ResponseWriter, r *http.Request) {
	id, err := domain.NewNodeID(r.PathValue("id"))
	if err != nil {
		respond(w, 0, nil, err)
		return
	}
	executionID, err := domain.NewExecutionID(r.PathValue("exec"))
	if err != nil {
		respond(w, 0, nil, err)
		return
	}
	var message nodeproto.ReportMessage
	if !readJSON(w, r, &message) {
		return
	}
	report, err := nodeproto.ToReport(message)
	if err != nil {
		respond(w, 0, nil, err)
		return
	}
	result, err := s.service.Runner().Report(r.Context(), id, executionID, report)
	if err != nil {
		respond(w, 0, nil, err)
		return
	}
	respond(w, http.StatusOK, nodeproto.ReportAck{Accepted: result.Accepted, Outcome: result.Execution.Outcome().String()}, nil)
}

// enroll takes a node's token and signing request and answers with its
// certificate: the one node route with no certificate behind it.
func (s *Server) enroll(w http.ResponseWriter, r *http.Request) {
	var request nodeproto.EnrollRequest
	if !readJSON(w, r, &request) {
		return
	}
	result, err := application.NewEnrollNode(s.service.Dependencies()).Execute(r.Context(), application.EnrollmentRequest{TokenID: request.TokenID, Secret: request.Secret, HostName: request.HostName, CSRPEM: []byte(request.CSR)})
	if err != nil {
		respond(w, 0, nil, err)
		return
	}
	respond(w, http.StatusCreated, nodeproto.EnrollResponse{Node: result.Node.Snapshot(), Certificate: string(result.CertificatePEM), CA: string(result.CAPEM), Serial: result.Serial, NotAfter: result.NotAfter}, nil)
}

func (s *Server) createEnrollmentToken(w http.ResponseWriter, r *http.Request) {
	var request EnrollTokenRequest
	if !readJSON(w, r, &request) {
		return
	}
	value, err := s.service.CreateEnrollmentToken(r.Context(), request)
	respond(w, http.StatusCreated, value, err)
}

func (s *Server) revokeNode(w http.ResponseWriter, r *http.Request) {
	value, err := s.service.RevokeNode(r.Context(), r.PathValue("id"))
	respond(w, http.StatusOK, value, err)
}

func createdStatus(created bool) int {
	if created {
		return http.StatusCreated
	}
	return http.StatusOK
}

// readJSON decodes the body, refusing unknown fields; false means the
// error was already written.
func readJSON(w http.ResponseWriter, r *http.Request, into any) bool {
	decoder := json.NewDecoder(io.LimitReader(r.Body, maxBodyBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(into); err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Kind: KindValidation, Message: "request body: " + err.Error()})
		return false
	}
	return true
}

// respond writes the result with the status, or the error classified.
func respond(w http.ResponseWriter, status int, value any, err error) {
	if err != nil {
		code, response := toWire(err)
		writeJSON(w, code, response)
		return
	}
	writeJSON(w, status, value)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
