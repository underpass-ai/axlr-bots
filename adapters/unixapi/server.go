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
	"strconv"
	"sync"
	"time"

	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
	"github.com/underpass-ai/axlr-bots/nodeproto"
)

// maxBodyBytes bounds a request body; a bot definition or an objective
// is far smaller. A webhook's event is bounded by the task input, an
// artifact by domain.MaxArtifactBytes, and a node's report, which
// carries the answer and the receipt, by maxReportBytes.
const (
	maxBodyBytes   = 1 << 20
	maxReportBytes = 8 << 20
)

// Server serves the operator's Service and the node protocol over HTTP:
// everything on a Unix socket (the same host), the node protocol alone
// on a TLS address (nodes on other hosts), where a client certificate
// issued by the fleet CA names the node and an enrollment token is the
// one way in without one, and the operator routes alone on an optional
// TLS address behind a bearer token.
type Server struct {
	service  *Local
	timeouts Timeouts
	// closing ends the node streams when the server shuts down: a
	// stream is never idle, so a graceful shutdown would wait for it.
	closing   chan struct{}
	closeOnce sync.Once
	// models answers the model and Jev calls of a node with no key.
	models *application.ModelProxy
	// wrap goes around each listener's handler (metrics); nil leaves
	// them as they are.
	wrap Middleware
}

// NewServer serves the local service.
func NewServer(service *Local) *Server {
	return &Server{service: service, timeouts: DefaultTimeouts, closing: make(chan struct{}), models: application.NewModelProxy(service.Dependencies())}
}

// Timeouts bound a connection: Read the whole request, uploads
// included, and Idle the wait for the next request on a kept-alive
// connection. There is no server-wide write timeout: the node stream
// lives for hours and a long poll answers after up to maxPollWait, so
// each response sets its own write deadline instead (writeJSON).
type Timeouts struct {
	Read time.Duration
	Idle time.Duration
}

// DefaultTimeouts gives a request two minutes, what a node takes to
// upload a 32-MiB artifact (nodeagent.DefaultUpload), and drops a
// connection idle for a minute.
var DefaultTimeouts = Timeouts{Read: 2 * time.Minute, Idle: time.Minute}

// WithTimeouts is the server with other timeouts; a zero field keeps
// the default.
func (s *Server) WithTimeouts(t Timeouts) *Server {
	if t.Read > 0 {
		s.timeouts.Read = t.Read
	}
	if t.Idle > 0 {
		s.timeouts.Idle = t.Idle
	}
	return s
}

// responseWriteTimeout bounds writing one JSON answer, and
// artifactWriteTimeout one artifact download: a client that stops
// reading no longer holds the handler for ever.
const (
	responseWriteTimeout = 30 * time.Second
	artifactWriteTimeout = 2 * time.Minute
)

// httpServer is an http.Server for the handler with the timeouts. A
// write deadline is set on the connection, and the server never resets
// it without a WriteTimeout, so each request starts with none: what the
// previous request on a kept-alive connection left would otherwise
// break the next answer, and kill a node stream outright.
func (s *Server) httpServer(handler http.Handler, config *tls.Config) *http.Server {
	fresh := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = http.NewResponseController(w).SetWriteDeadline(time.Time{})
		handler.ServeHTTP(w, r)
	})
	server := &http.Server{Handler: fresh, TLSConfig: config, ReadHeaderTimeout: 10 * time.Second, ReadTimeout: s.timeouts.Read, IdleTimeout: s.timeouts.Idle}
	if config != nil {
		server.ErrorLog = log.New(io.Discard, "", 0)
	}
	return server
}

// recheckEvery is how often a long poll looks again when nothing woke
// it: a change the control plane records wakes it at once, so this is
// for what changes without one (a deadline passing).
const recheckEvery = 5 * time.Second

// maxPollWait bounds a long poll.
const maxPollWait = 30 * time.Second

// Handler is the API's routes, all of them: what the socket serves.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	s.nodeRoutes(mux)
	s.operatorRoutes(mux)
	return mux
}

// NodeHandler is the node protocol alone: what the TLS address serves.
// A node certificate opens no operator route.
func (s *Server) NodeHandler() http.Handler {
	mux := http.NewServeMux()
	s.nodeRoutes(mux)
	return mux
}

// OperatorHandler is the operator's routes alone behind the bearer
// token, with the health check and the trigger routes open: what the
// operator address serves. No node route is on it.
func (s *Server) OperatorHandler(token string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	s.operatorRoutes(mux)
	return bearer(token, mux)
}

// operatorRoutes are the vision's minimal API (§11.1), the routes the
// command uses beside them, and the webhook triggers.
func (s *Server) operatorRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/build", s.getBuild)
	mux.HandleFunc("POST /v1/bots", s.registerBot)
	mux.HandleFunc("GET /v1/bots", s.listBots)
	mux.HandleFunc("GET /v1/bots/{id}", s.getBot)
	mux.HandleFunc("PATCH /v1/bots/{id}", s.patchBot)
	mux.HandleFunc("DELETE /v1/bots/{id}", s.deleteBot)
	mux.HandleFunc("POST /v1/bots/{id}/enable", s.enableBot)
	mux.HandleFunc("POST /v1/bots/{id}/pause", s.pauseBot)
	mux.HandleFunc("POST /v1/bots/{id}/stop", s.stopBot)
	mux.HandleFunc("PUT /v1/bots/{id}/staged", s.stageBot)
	mux.HandleFunc("GET /v1/bots/{id}/staged", s.getStagedBot)
	mux.HandleFunc("DELETE /v1/bots/{id}/staged", s.unstageBot)
	mux.HandleFunc("POST /v1/bots/{id}/staged/apply", s.applyStagedBot)
	mux.HandleFunc("POST /v1/bots/{id}/tasks", s.submitBotTask)
	mux.HandleFunc("GET /v1/bots/{id}/events", s.listEvents)
	mux.HandleFunc("GET /v1/groups", s.listGroups)
	mux.HandleFunc("GET /v1/groups/{name}", s.getGroup)
	mux.HandleFunc("GET /v1/groups/{name}/events", s.listGroupEvents)
	mux.HandleFunc("POST /v1/groups/{name}/events", s.publishGroupEvent)
	mux.HandleFunc("GET /v1/groups/{name}/topics", s.listGroupTopics)
	mux.HandleFunc("GET /v1/groups/{name}/links", s.listGroupLinks)
	mux.HandleFunc("POST /v1/tasks", s.submitTask)
	mux.HandleFunc("GET /v1/tasks", s.listTasks)
	mux.HandleFunc("GET /v1/tasks/{id}", s.getTask)
	mux.HandleFunc("GET /v1/tasks/{id}/events", s.listTaskEvents)
	mux.HandleFunc("GET /v1/tasks/{id}/executions", s.listExecutions)
	mux.HandleFunc("POST /v1/tasks/{id}/cancel", s.cancelTask)
	mux.HandleFunc("POST /v1/tasks/{id}/messages", s.sendTaskMessage)
	mux.HandleFunc("POST /v1/tasks/{id}/run", s.runTask)
	mux.HandleFunc("POST /v1/tasks/{id}/decide", s.decideTask)
	mux.HandleFunc("POST /v1/tasks/{id}/approve", s.decideAs(application.DecisionApprove))
	mux.HandleFunc("POST /v1/tasks/{id}/deny", s.decideAs(application.DecisionDeny))
	mux.HandleFunc("GET /v1/tasks/{id}/report", s.getReport)
	mux.HandleFunc("GET /v1/executions/{id}", s.getExecution)
	mux.HandleFunc("GET /v1/executions/{id}/artifacts/{name}", s.getArtifact)
	mux.HandleFunc("POST /v1/triggers/{bot}/{trigger}", s.fireTrigger)
	mux.HandleFunc("GET /v1/overview", s.getOverview)
	mux.HandleFunc("POST /v1/incidents", s.openIncident)
	mux.HandleFunc("GET /v1/incidents", s.listIncidents)
	mux.HandleFunc("GET /v1/incidents/{id}", s.getIncident)
	mux.HandleFunc("POST /v1/incidents/{id}/resolve", s.resolveIncident)
	mux.HandleFunc("POST /v1/incidents/{id}/diagnosis", s.markDiagnosis)
	mux.HandleFunc("GET /v1/autonomy", s.listAutonomy)
	mux.HandleFunc("GET /v1/autonomy/{id}", s.getAutonomy)
	mux.HandleFunc("POST /v1/autonomy/{id}/lower", s.lowerAutonomy)
	mux.HandleFunc("POST /v1/autonomy/{id}/clear", s.clearAutonomy)
	mux.HandleFunc("POST /v1/incidents/{id}/postmortem/approve", s.reviewPostmortem(s.service.ApprovePostmortem))
	mux.HandleFunc("POST /v1/incidents/{id}/postmortem/send-back", s.reviewPostmortem(s.service.SendBackPostmortem))
	mux.HandleFunc("GET /v1/outputs", s.listOutputs)
	mux.HandleFunc("POST /v1/outputs", s.addOutput)
	mux.HandleFunc("DELETE /v1/outputs/{name}", s.removeOutput)
	mux.HandleFunc("GET /v1/nodes", s.listNodes)
	mux.HandleFunc("GET /v1/nodes/{id}", s.getNode)
	mux.HandleFunc("POST /v1/enrollments", s.createEnrollmentToken)
	mux.HandleFunc("POST /v1/nodes/{id}/revoke", s.revokeNode)
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
	mux.HandleFunc("POST /v1/nodes/{id}/certificate", s.authenticated(s.renewCertificate))
	mux.HandleFunc("GET /v1/nodes/{id}/assignments/next", s.authenticated(s.nextAssignment))
	mux.HandleFunc("GET /v1/nodes/{id}/stream", s.authenticated(s.stream))
	mux.HandleFunc("POST /v1/nodes/{id}/executions/{exec}/lease", s.authenticated(s.renewLease))
	mux.HandleFunc("PUT /v1/nodes/{id}/executions/{exec}/artifacts/{name}", s.authenticated(s.uploadArtifact))
	mux.HandleFunc("POST /v1/nodes/{id}/executions/{exec}/report", s.authenticated(s.report))
	mux.HandleFunc("POST /v1/nodes/{id}/executions/{exec}/delegations", s.authenticated(s.delegate))
	mux.HandleFunc("GET /v1/nodes/{id}/executions/{exec}/delegations/{task}", s.authenticated(s.awaitDelegation))
	mux.HandleFunc("POST /v1/nodes/{id}/executions/{exec}/questions", s.authenticated(s.ask))
	mux.HandleFunc("GET /v1/nodes/{id}/executions/{exec}/questions/{task}", s.authenticated(s.questionState))
	mux.HandleFunc("POST /v1/nodes/{id}/executions/{exec}/watch", s.authenticated(s.watchNext))
	mux.HandleFunc("POST /v1/nodes/{id}/executions/{exec}/watch/decisions", s.authenticated(s.decideWatch))
	mux.HandleFunc("POST /v1/nodes/{id}/executions/{exec}/events", s.authenticated(s.emit))
	mux.HandleFunc("POST /v1/nodes/{id}/executions/{exec}/subscriptions", s.authenticated(s.subscribe))
	mux.HandleFunc("GET /v1/nodes/{id}/executions/{exec}/messages", s.authenticated(s.executionMessages))
	mux.HandleFunc("POST /v1/nodes/{id}/executions/{exec}/said", s.authenticated(s.said))
	mux.HandleFunc("POST /v1/nodes/{id}/executions/{exec}/model", s.authenticated(s.proxyModel))
	mux.HandleFunc("POST /v1/nodes/{id}/executions/{exec}/jev", s.authenticated(s.proxyJev))
	mux.HandleFunc("POST /v1/nodes/{id}/executions/{exec}/progress", s.authenticated(s.progress))
}

// ServeTLS serves the node protocol on the listener with the TLS
// configuration (pki.ServerTLSConfig) until ctx ends. The caller binds
// the listener, so it knows the address when it asked for port 0.
func (s *Server) ServeTLS(ctx context.Context, listener net.Listener, config *tls.Config) error {
	server := s.httpServer(s.wrapped("nodes", s.NodeHandler()), config)
	return s.run(ctx, server, func() error { return server.ServeTLS(listener, "", "") }, func() {})
}

// ServeAPI serves the operator routes on the listener over TLS behind
// the bearer token until ctx ends.
func (s *Server) ServeAPI(ctx context.Context, listener net.Listener, config *tls.Config, token string) error {
	if token == "" {
		return errors.New("unixapi: the operator address needs a bearer token")
	}
	server := s.httpServer(s.wrapped("operator", s.OperatorHandler(token)), config)
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
	server := s.httpServer(s.wrapped("socket", s.Handler()), nil)
	return s.run(ctx, server, func() error { return server.Serve(listener) }, cleanup)
}

// run drives one server until ctx ends or it stops on its own.
func (s *Server) run(ctx context.Context, server *http.Server, start func() error, cleanup func()) error {
	done := make(chan error, 1)
	go func() { done <- start() }()
	select {
	case <-ctx.Done():
		s.closeOnce.Do(func() { close(s.closing) })
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

func (s *Server) listGroups(w http.ResponseWriter, r *http.Request) {
	value, err := s.service.ListGroups(r.Context())
	respond(w, http.StatusOK, value, err)
}

func (s *Server) getGroup(w http.ResponseWriter, r *http.Request) {
	value, err := s.service.GetGroup(r.Context(), r.PathValue("name"), r.URL.Query().Get("tenant"))
	respond(w, http.StatusOK, value, err)
}

// listGroupEvents is GET /v1/groups/{name}/events?after=&limit=&wait=:
// the team's timeline after the caller's last event; with wait, it
// answers as soon as there is a new one, or empty when wait passes.
func (s *Server) listGroupEvents(w http.ResponseWriter, r *http.Request) {
	limit, err := queryLimit(r)
	if err != nil {
		respond(w, 0, nil, err)
		return
	}
	wait, err := queryWait(r)
	if err != nil {
		respond(w, 0, nil, err)
		return
	}
	query := application.GroupEventsQuery{Name: r.PathValue("name"), Tenant: r.URL.Query().Get("tenant"), After: r.URL.Query().Get("after"), Limit: limit}
	s.pollEvents(w, r, wait, func() ([]domain.Event, error) { return s.service.ListGroupEvents(r.Context(), query) })
}

// pollEvents answers what read finds: at once when it finds an event or
// fails, else as soon as a change brings one, or [] when wait passes.
func (s *Server) pollEvents(w http.ResponseWriter, r *http.Request, wait time.Duration, read func() ([]domain.Event, error)) {
	deadline := time.Now().Add(wait)
	for {
		changed, _ := s.service.changes.next()
		events, err := read()
		if err != nil || len(events) > 0 || !time.Now().Before(deadline) {
			respond(w, http.StatusOK, events, err)
			return
		}
		if !awaitChange(r.Context(), changed, min(recheckEvery, time.Until(deadline))) {
			respond(w, http.StatusOK, events, nil)
			return
		}
	}
}

// queryWait reads the wait parameter of a long poll, at most
// maxPollWait: zero when it is absent.
func queryWait(r *http.Request) (time.Duration, error) {
	raw := r.URL.Query().Get("wait")
	if raw == "" {
		return 0, nil
	}
	parsed, err := time.ParseDuration(raw)
	if err != nil || parsed < 0 {
		return 0, domain.ValidationError{Field: "wait", Reason: "must be a duration"}
	}
	return min(parsed, maxPollWait), nil
}

// publishGroupEvent is POST /v1/groups/{name}/events?tenant=: the
// operator publishes on the team, the key in the Idempotency-Key header
// or the body; 201 for a new event, 200 for the one the key published.
func (s *Server) publishGroupEvent(w http.ResponseWriter, r *http.Request) {
	var command application.PublishGroupEventCommand
	if !readJSON(w, r, &command) {
		return
	}
	command.Group, command.Tenant = r.PathValue("name"), r.URL.Query().Get("tenant")
	if key := r.Header.Get(IdempotencyKeyHeader); key != "" {
		command.IdempotencyKey = key
	}
	answer, err := s.service.PublishGroupEvent(r.Context(), command)
	respond(w, createdStatus(answer.Created), answer, err)
}

// listGroupTopics is GET /v1/groups/{name}/topics?tenant=.
func (s *Server) listGroupTopics(w http.ResponseWriter, r *http.Request) {
	value, err := s.service.ListGroupTopics(r.Context(), r.PathValue("name"), r.URL.Query().Get("tenant"))
	respond(w, http.StatusOK, value, err)
}

// listGroupLinks is GET /v1/groups/{name}/links?tenant=.
func (s *Server) listGroupLinks(w http.ResponseWriter, r *http.Request) {
	value, err := s.service.ListGroupLinks(r.Context(), r.PathValue("name"), r.URL.Query().Get("tenant"))
	respond(w, http.StatusOK, value, err)
}

func (s *Server) enableBot(w http.ResponseWriter, r *http.Request) {
	value, err := s.service.EnableBot(r.Context(), r.PathValue("id"))
	respond(w, http.StatusOK, value, err)
}

func (s *Server) pauseBot(w http.ResponseWriter, r *http.Request) {
	value, err := s.service.PauseBot(r.Context(), r.PathValue("id"))
	respond(w, http.StatusOK, value, err)
}

func (s *Server) stopBot(w http.ResponseWriter, r *http.Request) {
	value, err := s.service.StopBot(r.Context(), r.PathValue("id"))
	respond(w, http.StatusOK, value, err)
}

func (s *Server) deleteBot(w http.ResponseWriter, r *http.Request) {
	value, err := s.service.DeleteBot(r.Context(), r.PathValue("id"))
	respond(w, http.StatusOK, value, err)
}

func (s *Server) patchBot(w http.ResponseWriter, r *http.Request) {
	var patch application.BotPatch
	if !readJSON(w, r, &patch) {
		return
	}
	value, err := s.service.UpdateBot(r.Context(), r.PathValue("id"), patch)
	respond(w, http.StatusOK, value, err)
}

// stageBot is PUT /v1/bots/{id}/staged: the patch the update takes,
// naming its version, held for later.
func (s *Server) stageBot(w http.ResponseWriter, r *http.Request) {
	var patch application.BotPatch
	if !readJSON(w, r, &patch) {
		return
	}
	value, err := s.service.StageBot(r.Context(), r.PathValue("id"), patch)
	respond(w, http.StatusOK, value, err)
}

func (s *Server) getStagedBot(w http.ResponseWriter, r *http.Request) {
	value, err := s.service.GetStagedBot(r.Context(), r.PathValue("id"))
	respond(w, http.StatusOK, value, err)
}

func (s *Server) applyStagedBot(w http.ResponseWriter, r *http.Request) {
	value, err := s.service.ApplyStagedBot(r.Context(), r.PathValue("id"))
	respond(w, http.StatusOK, value, err)
}

func (s *Server) unstageBot(w http.ResponseWriter, r *http.Request) {
	value, err := s.service.UnstageBot(r.Context(), r.PathValue("id"))
	respond(w, http.StatusOK, value, err)
}

// submitTask is POST /v1/tasks: the bot in the body, the key in the
// Idempotency-Key header or the body.
func (s *Server) submitTask(w http.ResponseWriter, r *http.Request) {
	var request SubmitRequest
	if !readJSON(w, r, &request) {
		return
	}
	if key := r.Header.Get(IdempotencyKeyHeader); key != "" {
		request.IdempotencyKey = key
	}
	response, err := s.service.SubmitTask(r.Context(), request)
	respond(w, createdStatus(response.Created), response, err)
}

// submitBotTask is POST /v1/bots/{id}/tasks: the bot in the path, the
// key in the Idempotency-Key header or the body.
func (s *Server) submitBotTask(w http.ResponseWriter, r *http.Request) {
	var request TaskRequest
	if !readJSON(w, r, &request) {
		return
	}
	key := r.Header.Get(IdempotencyKeyHeader)
	if key == "" {
		key = request.IdempotencyKey
	}
	response, err := s.service.SubmitTask(r.Context(), SubmitRequest{BotID: r.PathValue("id"), Objective: request.Objective, IdempotencyKey: key, CorrelationID: request.CorrelationID, Priority: request.Priority})
	respond(w, createdStatus(response.Created), response, err)
}

func (s *Server) listEvents(w http.ResponseWriter, r *http.Request) {
	limit, err := queryLimit(r)
	if err != nil {
		respond(w, 0, nil, err)
		return
	}
	value, err := s.service.ListEvents(r.Context(), r.PathValue("id"), limit)
	respond(w, http.StatusOK, value, err)
}

// listTasks is GET /v1/tasks?bot=&status=&since=&limit=: the tasks
// across bots, newest first.
func (s *Server) listTasks(w http.ResponseWriter, r *http.Request) {
	limit, err := queryLimit(r)
	if err != nil {
		respond(w, 0, nil, err)
		return
	}
	since, err := querySince(r)
	if err != nil {
		respond(w, 0, nil, err)
		return
	}
	query := application.TaskQuery{BotID: r.URL.Query().Get("bot"), Status: r.URL.Query().Get("status"), Since: since, Limit: limit}
	value, err := s.service.ListTasks(r.Context(), query)
	respond(w, http.StatusOK, value, err)
}

// listTaskEvents is GET /v1/tasks/{id}/events?after=&limit=&wait=: the
// task's timeline after the caller's last event, waiting for a new one
// as a team's timeline does.
func (s *Server) listTaskEvents(w http.ResponseWriter, r *http.Request) {
	limit, err := queryLimit(r)
	if err != nil {
		respond(w, 0, nil, err)
		return
	}
	wait, err := queryWait(r)
	if err != nil {
		respond(w, 0, nil, err)
		return
	}
	query := application.TaskEventsQuery{ID: domain.TaskID(r.PathValue("id")), After: r.URL.Query().Get("after"), Limit: limit}
	s.pollEvents(w, r, wait, func() ([]domain.Event, error) { return s.service.ListTaskEvents(r.Context(), query) })
}

// getOverview is GET /v1/overview?since=: the dashboard's counts.
func (s *Server) getOverview(w http.ResponseWriter, r *http.Request) {
	since, err := querySince(r)
	if err != nil {
		respond(w, 0, nil, err)
		return
	}
	value, err := s.service.GetOverview(r.Context(), since)
	respond(w, http.StatusOK, value, err)
}

// queryLimit reads the limit parameter: zero when it is absent.
func queryLimit(r *http.Request) (int, error) {
	raw := r.URL.Query().Get("limit")
	if raw == "" {
		return 0, nil
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil || parsed < 0 {
		return 0, domain.ValidationError{Field: "limit", Reason: "must be a number"}
	}
	return parsed, nil
}

// querySince reads the since parameter as an RFC 3339 time: zero when it
// is absent.
func querySince(r *http.Request) (time.Time, error) {
	raw := r.URL.Query().Get("since")
	if raw == "" {
		return time.Time{}, nil
	}
	since, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return time.Time{}, domain.ValidationError{Field: "since", Reason: "must be an RFC 3339 time"}
	}
	return since, nil
}

func (s *Server) listExecutions(w http.ResponseWriter, r *http.Request) {
	value, err := s.service.ListExecutions(r.Context(), r.PathValue("id"))
	respond(w, http.StatusOK, value, err)
}

// sendTaskMessage is POST /v1/tasks/{id}/messages: the operator writes to
// a running task.
func (s *Server) sendTaskMessage(w http.ResponseWriter, r *http.Request) {
	var request MessageRequest
	if !readJSON(w, r, &request) {
		return
	}
	value, err := s.service.SendTaskMessage(r.Context(), r.PathValue("id"), request)
	respond(w, http.StatusOK, value, err)
}

// executionMessages is GET .../executions/{exec}/messages?after=<id>: the
// operator's messages for the node's running execution.
func (s *Server) executionMessages(w http.ResponseWriter, r *http.Request) {
	id, executionID, ok := nodeExecution(w, r)
	if !ok {
		return
	}
	messages, err := application.NewExecutionMessages(s.service.Dependencies()).Execute(r.Context(), id, executionID, r.URL.Query().Get("after"))
	if err != nil {
		respond(w, 0, nil, err)
		return
	}
	respond(w, http.StatusOK, nodeproto.MessagesMessage{Messages: messages}, nil)
}

// said is POST .../executions/{exec}/said: what the node's run said back
// to the operator's message.
func (s *Server) said(w http.ResponseWriter, r *http.Request) {
	id, executionID, ok := nodeExecution(w, r)
	if !ok {
		return
	}
	var reply application.SaidReply
	if !readJSON(w, r, &reply) {
		return
	}
	err := application.NewRecordSaid(s.service.Dependencies()).Execute(r.Context(), id, executionID, reply)
	respond(w, http.StatusOK, map[string]bool{"recorded": err == nil}, err)
}

// progress is POST .../executions/{exec}/progress: what the node's run
// did since the last batch.
func (s *Server) progress(w http.ResponseWriter, r *http.Request) {
	id, executionID, ok := nodeExecution(w, r)
	if !ok {
		return
	}
	var batch application.ProgressBatch
	if !readJSON(w, r, &batch) {
		return
	}
	answer, err := application.NewRecordProgress(s.service.Dependencies()).Execute(r.Context(), id, executionID, batch)
	respond(w, http.StatusOK, answer, err)
}

// openIncident is POST /v1/incidents: an alert group. 201 for a new
// incident, 200 for a repeat or a reopening.
func (s *Server) openIncident(w http.ResponseWriter, r *http.Request) {
	var command application.OpenIncidentCommand
	if !readJSON(w, r, &command) {
		return
	}
	response, err := s.service.OpenIncident(r.Context(), command)
	respond(w, createdStatus(response.Created), response, err)
}

func (s *Server) listIncidents(w http.ResponseWriter, r *http.Request) {
	limit, err := queryLimit(r)
	if err != nil {
		respond(w, 0, nil, err)
		return
	}
	query := application.IncidentQuery{Tenant: r.URL.Query().Get("tenant"), Status: r.URL.Query().Get("status"), Limit: limit}
	value, err := s.service.ListIncidents(r.Context(), query)
	respond(w, http.StatusOK, value, err)
}

func (s *Server) getIncident(w http.ResponseWriter, r *http.Request) {
	value, err := s.service.GetIncident(r.Context(), r.PathValue("id"))
	respond(w, http.StatusOK, value, err)
}

// resolveIncident is POST /v1/incidents/{id}/resolve; the body, with
// the reason, may be empty.
func (s *Server) resolveIncident(w http.ResponseWriter, r *http.Request) {
	var request ResolveRequest
	if r.ContentLength != 0 && !readJSON(w, r, &request) {
		return
	}
	value, err := s.service.ResolveIncident(r.Context(), r.PathValue("id"), request)
	respond(w, http.StatusOK, value, err)
}

// markDiagnosis is POST /v1/incidents/{id}/diagnosis: a person's verdict
// on the diagnosis of the incident's current cycle.
func (s *Server) markDiagnosis(w http.ResponseWriter, r *http.Request) {
	var request DiagnosisRequest
	if !readJSON(w, r, &request) {
		return
	}
	header := r.Header.Get(ActorHeader)
	if request.MarkedBy != "" && header != "" && request.MarkedBy != header {
		respond(w, 0, nil, domain.ValidationError{Field: "markedBy", Reason: "names another actor than " + ActorHeader})
		return
	}
	if request.MarkedBy == "" {
		request.MarkedBy = header
	}
	value, err := s.service.MarkDiagnosis(r.Context(), r.PathValue("id"), request)
	respond(w, http.StatusOK, value, err)
}

// listAutonomy is GET /v1/autonomy, narrowed by ?tenant=, ?bot=,
// ?runbook= and ?namespace=.
func (s *Server) listAutonomy(w http.ResponseWriter, r *http.Request) {
	values := r.URL.Query()
	query := application.AutonomyQuery{Tenant: values.Get("tenant"), Bot: values.Get("bot"), Runbook: values.Get("runbook"), Namespace: values.Get("namespace")}
	value, err := s.service.ListAutonomy(r.Context(), query)
	respond(w, http.StatusOK, value, err)
}

func (s *Server) getAutonomy(w http.ResponseWriter, r *http.Request) {
	value, err := s.service.GetAutonomy(r.Context(), r.PathValue("id"))
	respond(w, http.StatusOK, value, err)
}

// clearAutonomy is POST /v1/autonomy/{id}/clear: a person ends a
// runbook's outstanding applies.
func (s *Server) clearAutonomy(w http.ResponseWriter, r *http.Request) {
	var request ClearRequest
	if r.ContentLength != 0 && !readJSON(w, r, &request) {
		return
	}
	header := r.Header.Get(ActorHeader)
	if request.ClearedBy != "" && header != "" && request.ClearedBy != header {
		respond(w, 0, nil, domain.ValidationError{Field: "clearedBy", Reason: "names another actor than " + ActorHeader})
		return
	}
	if request.ClearedBy == "" {
		request.ClearedBy = header
	}
	value, err := s.service.ClearAutonomy(r.Context(), r.PathValue("id"), request)
	respond(w, http.StatusOK, value, err)
}

// lowerAutonomy is POST /v1/autonomy/{id}/lower: a person takes a runbook
// one step down.
func (s *Server) lowerAutonomy(w http.ResponseWriter, r *http.Request) {
	var request LowerRequest
	if r.ContentLength != 0 && !readJSON(w, r, &request) {
		return
	}
	header := r.Header.Get(ActorHeader)
	if request.LoweredBy != "" && header != "" && request.LoweredBy != header {
		respond(w, 0, nil, domain.ValidationError{Field: "loweredBy", Reason: "names another actor than " + ActorHeader})
		return
	}
	if request.LoweredBy == "" {
		request.LoweredBy = header
	}
	value, err := s.service.LowerAutonomy(r.Context(), r.PathValue("id"), request)
	respond(w, http.StatusOK, value, err)
}

// reviewPostmortem is POST /v1/incidents/{id}/postmortem/approve and
// /send-back; the body may be empty when the header names who answers.
func (s *Server) reviewPostmortem(review func(context.Context, string, PostmortemReviewRequest) (domain.IncidentSnapshot, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var request PostmortemReviewRequest
		if r.ContentLength != 0 && !readJSON(w, r, &request) {
			return
		}
		header := r.Header.Get(ActorHeader)
		if request.DecidedBy != "" && header != "" && request.DecidedBy != header {
			respond(w, 0, nil, domain.ValidationError{Field: "decidedBy", Reason: "names another actor than " + ActorHeader})
			return
		}
		if request.DecidedBy == "" {
			request.DecidedBy = header
		}
		value, err := review(r.Context(), r.PathValue("id"), request)
		respond(w, http.StatusOK, value, err)
	}
}

// addOutput is POST /v1/outputs: a new incident output target, 201;
// the answer has a fingerprint, never the URL or the secret.
func (s *Server) addOutput(w http.ResponseWriter, r *http.Request) {
	var definition application.OutputDefinition
	if !readJSON(w, r, &definition) {
		return
	}
	value, err := s.service.AddOutput(r.Context(), definition)
	respond(w, http.StatusCreated, value, err)
}

func (s *Server) listOutputs(w http.ResponseWriter, r *http.Request) {
	value, err := s.service.ListOutputs(r.Context())
	respond(w, http.StatusOK, value, err)
}

// removeOutput is DELETE /v1/outputs/{name}; its pending deliveries are
// skipped.
func (s *Server) removeOutput(w http.ResponseWriter, r *http.Request) {
	value, err := s.service.RemoveOutput(r.Context(), r.PathValue("name"))
	respond(w, http.StatusOK, value, err)
}

func (s *Server) cancelTask(w http.ResponseWriter, r *http.Request) {
	value, err := s.service.CancelTask(r.Context(), r.PathValue("id"))
	respond(w, http.StatusOK, value, err)
}

// fireTrigger is POST /v1/triggers/{bot}/{trigger}: the body is the
// event. Over TLS (the operator address) the request must carry the
// trigger's secret or a signature of the body under its signing key;
// over the socket the caller is trusted, as for every other route. A
// cloudwatch trigger takes Amazon SNS's message (text/plain, its type in
// x-amz-sns-message-type) and is authenticated by SNS's signature, which
// the application verifies on every route.
func (s *Server) fireTrigger(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, domain.MaxTaskInputBytes))
	if err != nil {
		respond(w, 0, nil, err)
		return
	}
	botID, trigger := r.PathValue("bot"), r.PathValue("trigger")
	if r.TLS != nil {
		if err := s.authenticateWebhook(r, botID, trigger, body); err != nil {
			respond(w, 0, nil, err)
			return
		}
	}
	response, err := s.service.FireTrigger(r.Context(), FireRequest{BotID: botID, Trigger: trigger, DeliveryID: r.Header.Get(IdempotencyKeyHeader), Body: body, MessageType: r.Header.Get(SNSMessageTypeHeader)})
	respond(w, createdStatus(response.Created), response, err)
}

// authenticateWebhook checks the request against the trigger's
// credentials. A missing bot or trigger is refused as forbidden too, so
// the route tells a stranger nothing.
func (s *Server) authenticateWebhook(r *http.Request, botID, trigger string, body []byte) error {
	refused := domain.ForbiddenError{Reason: "the trigger's secret or signature is required"}
	id, err := domain.NewBotID(botID)
	if err != nil {
		return refused
	}
	bot, err := s.service.Dependencies().Bots.Get(r.Context(), id)
	if err != nil {
		return refused
	}
	webhook, ok := bot.Trigger(trigger)
	if !ok {
		return refused
	}
	if webhook.Type() == domain.TriggerCloudWatch {
		// SNS sets no header of ours: its signature, verified by the
		// application, is the credential.
		return nil
	}
	signature := r.Header.Get(WebhookSignatureHeader)
	if signature == "" {
		signature = r.Header.Get("X-Hub-Signature-256")
	}
	if !webhook.Authenticates(r.Header.Get(WebhookSecretHeader), signature, body) {
		return refused
	}
	return nil
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
	header := r.Header.Get(ActorHeader)
	if request.DecidedBy != "" && header != "" && request.DecidedBy != header {
		respond(w, 0, nil, domain.ValidationError{Field: "decidedBy", Reason: "names another actor than " + ActorHeader})
		return
	}
	if request.DecidedBy == "" {
		request.DecidedBy = header
	}
	value, err := s.service.DecideTask(r.Context(), r.PathValue("id"), request)
	respond(w, http.StatusOK, value, err)
}

// decideAs is POST /v1/tasks/{id}/approve and /deny: the decision is the
// route, the pending call the optional ?call= (what a notification's
// links carry), who decides the X-Axlr-Actor header, and no body is
// read.
func (s *Server) decideAs(decision string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		request := DecideRequest{Decision: decision, CallID: r.URL.Query().Get("call"), DecidedBy: r.Header.Get(ActorHeader)}
		value, err := s.service.DecideTask(r.Context(), r.PathValue("id"), request)
		respond(w, http.StatusOK, value, err)
	}
}

// getArtifact is GET /v1/executions/{id}/artifacts/{name}: the bytes of
// an artifact the control plane keeps, with its media type, size and
// hash in the headers, as a download.
func (s *Server) getArtifact(w http.ResponseWriter, r *http.Request) {
	content, err := s.service.GetArtifact(r.Context(), r.PathValue("id"), r.PathValue("name"))
	if err != nil {
		respond(w, 0, nil, err)
		return
	}
	defer content.Body.Close()
	artifact := content.Artifact
	mediaType := artifact.MediaType
	if domain.ValidateMediaType(mediaType) != nil {
		mediaType = application.DefaultArtifactMediaType
	}
	w.Header().Set("Content-Type", mediaType)
	w.Header().Set("Content-Length", strconv.FormatInt(artifact.Size, 10))
	w.Header().Set("Content-Disposition", `attachment; filename="`+artifact.Name+`"`)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set(nodeproto.ArtifactSHA256Header, artifact.SHA256)
	writeDeadline(w, artifactWriteTimeout)
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, content.Body)
}

func (s *Server) getReport(w http.ResponseWriter, r *http.Request) {
	value, err := s.service.GetReport(r.Context(), r.PathValue("id"))
	respond(w, http.StatusOK, value, err)
}

// getBuild answers which build the control plane runs and whether a
// newer one is installed.
func (s *Server) getBuild(w http.ResponseWriter, _ *http.Request) {
	deps := s.service.Dependencies()
	writeJSON(w, http.StatusOK, newBuildAnswer(application.CheckBuild(deps.Build), deps.MinimumAXLR))
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
	registration := application.NodeRegistration{ID: request.ID, HostName: request.HostName, Labels: request.Labels, Capabilities: request.Capabilities}
	if identity, ok := identityOf(r); ok {
		if registration.ID == "" {
			registration.ID = identity.ID.String()
		}
		if registration.ID != identity.ID.String() {
			respond(w, 0, nil, domain.ForbiddenError{Reason: "the certificate is for node " + identity.ID.String() + ", the registration names " + registration.ID})
			return
		}
		// A node the control plane no longer knows is pinned to the
		// certificate it registers with; without the serial its next
		// call would be refused as not the current one.
		registration.CertificateSerial, registration.CertificateNotAfter = identity.Serial, identity.NotAfter
	}
	node, err := application.NewRegisterNode(s.service.Dependencies()).Execute(r.Context(), registration)
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

// nextAssignment long-polls: it looks for an assignment, and again each
// time the control plane records a change that may give the node one (at
// least every recheckEvery), until one appears or wait passes, then
// answers 204.
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
		changed := s.service.changes.nextWork()
		assignment, found, err := s.service.Runner().NextAssignment(r.Context(), id)
		if err != nil {
			respond(w, 0, nil, err)
			return
		}
		if found {
			respond(w, http.StatusOK, nodeproto.FromAssignment(assignment), nil)
			return
		}
		if !time.Now().Before(deadline) || !awaitChange(r.Context(), changed, min(recheckEvery, time.Until(deadline))) {
			writeDeadline(w, responseWriteTimeout)
			w.WriteHeader(http.StatusNoContent)
			return
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
	if !readJSONLimit(w, r, &message, maxReportBytes) {
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
	respond(w, http.StatusOK, nodeproto.ReportAck{Accepted: result.Accepted, Outcome: result.Execution.Outcome().String(), ChannelMemory: nodeproto.FromChannelRecord(result.ChannelMemory)}, nil)
}

// delegate is POST /v1/nodes/{id}/executions/{exec}/delegations: the
// execution asks another bot to do a task. A refusal is a 200 answer
// with refused set; the node hands it to the model.
func (s *Server) delegate(w http.ResponseWriter, r *http.Request) {
	id, executionID, ok := nodeExecution(w, r)
	if !ok {
		return
	}
	var message nodeproto.DelegateMessage
	if !readJSON(w, r, &message) {
		return
	}
	answer, err := s.service.Runner().Delegate(r.Context(), id, executionID, application.DelegateCommand{Bot: message.Bot, Objective: message.Objective, Wait: message.Wait})
	if err != nil {
		respond(w, 0, nil, err)
		return
	}
	status := http.StatusOK
	if answer.Created {
		status = http.StatusCreated
	}
	respond(w, status, nodeproto.FromDelegation(answer), nil)
}

// ask is POST /v1/nodes/{id}/executions/{exec}/questions: the execution
// asks another team a question. It answers at once, 201 with the
// question's task or 200 with the same ask's or a refusal; the answer
// comes back to the bot later.
func (s *Server) ask(w http.ResponseWriter, r *http.Request) {
	id, executionID, ok := nodeExecution(w, r)
	if !ok {
		return
	}
	var message nodeproto.AskMessage
	if !readJSON(w, r, &message) {
		return
	}
	answer, err := s.service.Runner().Ask(r.Context(), id, executionID, application.AskTeamCommand{Team: message.Team, Question: message.Question})
	if err != nil {
		respond(w, 0, nil, err)
		return
	}
	status := http.StatusOK
	if answer.Created {
		status = http.StatusCreated
	}
	respond(w, status, nodeproto.FromQuestion(answer), nil)
}

// questionState is GET .../questions/{task}: where a question the
// execution's task asked stands, from the control plane's records.
func (s *Server) questionState(w http.ResponseWriter, r *http.Request) {
	id, executionID, ok := nodeExecution(w, r)
	if !ok {
		return
	}
	question, err := domain.NewTaskID(r.PathValue("task"))
	if err != nil {
		respond(w, 0, nil, err)
		return
	}
	view, err := application.NewGetQuestion(s.service.Dependencies()).Execute(r.Context(), id, executionID, question)
	if err != nil {
		respond(w, 0, nil, err)
		return
	}
	respond(w, http.StatusOK, nodeproto.FromQuestionView(view), nil)
}

// watchNext is POST .../watch: a team's Jev watcher asks for the next
// question of its round after the cursor. Only a Jev watcher is answered,
// and only with its own team's questions.
func (s *Server) watchNext(w http.ResponseWriter, r *http.Request) {
	id, executionID, ok := nodeExecution(w, r)
	if !ok {
		return
	}
	var message nodeproto.WatchMessage
	if !readJSON(w, r, &message) {
		return
	}
	after := domain.TaskID("")
	if message.After != "" {
		cursor, err := domain.NewTaskID(message.After)
		if err != nil {
			respond(w, 0, nil, err)
			return
		}
		after = cursor
	}
	watched, err := s.service.Runner().WatchNext(r.Context(), id, executionID, after, message.Tasks)
	if err != nil {
		respond(w, 0, nil, err)
		return
	}
	respond(w, http.StatusOK, nodeproto.FromWatched(watched), nil)
}

// decideWatch is POST .../watch/decisions: the watcher's decision for one
// question of its team; abandon cancels it.
func (s *Server) decideWatch(w http.ResponseWriter, r *http.Request) {
	id, executionID, ok := nodeExecution(w, r)
	if !ok {
		return
	}
	var message nodeproto.WatchDecisionMessage
	if !readJSON(w, r, &message) {
		return
	}
	question, err := domain.NewTaskID(message.Question)
	if err != nil {
		respond(w, 0, nil, err)
		return
	}
	decision := application.WatchDecision{Question: question, Choice: message.Choice, Probability: message.Probability, Confidence: message.Confidence}
	decided, err := s.service.Runner().DecideWatch(r.Context(), id, executionID, decision)
	if err != nil {
		respond(w, 0, nil, err)
		return
	}
	respond(w, http.StatusOK, nodeproto.FromWatchDecided(decided), nil)
}

// emit is POST .../events: the execution tells its teams an event.
func (s *Server) emit(w http.ResponseWriter, r *http.Request) {
	id, executionID, ok := nodeExecution(w, r)
	if !ok {
		return
	}
	var message nodeproto.EmitMessage
	if !readJSON(w, r, &message) {
		return
	}
	command := application.EmitEventCommand{Name: message.Name, Message: message.Message, Severity: message.Severity, Data: message.Data, To: message.To}
	answer, err := s.service.Runner().Emit(r.Context(), id, executionID, command)
	if err != nil {
		respond(w, 0, nil, err)
		return
	}
	respond(w, http.StatusOK, nodeproto.FromEmitted(answer), nil)
}

// subscribe is POST .../subscriptions: the execution's bot asks to hear
// a topic of its teams; the answer is 200, a refusal included.
func (s *Server) subscribe(w http.ResponseWriter, r *http.Request) {
	id, executionID, ok := nodeExecution(w, r)
	if !ok {
		return
	}
	var message nodeproto.SubscribeMessage
	if !readJSON(w, r, &message) {
		return
	}
	answer, err := s.service.Runner().Subscribe(r.Context(), id, executionID, application.SubscribeCommand{Topic: message.Topic, Why: message.Why})
	if err != nil {
		respond(w, 0, nil, err)
		return
	}
	respond(w, http.StatusOK, nodeproto.FromSubscribed(answer), nil)
}

// awaitDelegation is GET .../delegations/{task}?wait=20s: it answers the
// child's state at once when it has ended, or when wait passes.
func (s *Server) awaitDelegation(w http.ResponseWriter, r *http.Request) {
	id, executionID, ok := nodeExecution(w, r)
	if !ok {
		return
	}
	child, err := domain.NewTaskID(r.PathValue("task"))
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
	read := application.NewGetDelegation(s.service.Dependencies())
	for {
		changed, _ := s.service.changes.next()
		answer, err := read.Execute(r.Context(), id, executionID, child)
		if err != nil {
			respond(w, 0, nil, err)
			return
		}
		if answer.Done || !time.Now().Before(deadline) || !awaitChange(r.Context(), changed, min(recheckEvery, time.Until(deadline))) {
			respond(w, http.StatusOK, nodeproto.FromDelegation(answer), nil)
			return
		}
	}
}

// nodeExecution reads the node and execution ids of a node route.
func nodeExecution(w http.ResponseWriter, r *http.Request) (domain.NodeID, domain.ExecutionID, bool) {
	id, err := domain.NewNodeID(r.PathValue("id"))
	if err != nil {
		respond(w, 0, nil, err)
		return "", "", false
	}
	executionID, err := domain.NewExecutionID(r.PathValue("exec"))
	if err != nil {
		respond(w, 0, nil, err)
		return "", "", false
	}
	return id, executionID, true
}

// uploadArtifact is PUT /v1/nodes/{id}/executions/{exec}/artifacts/{name}:
// the body is the artifact's bytes, its size the Content-Length, its
// media type the Content-Type and its hash the SHA-256 header.
func (s *Server) uploadArtifact(w http.ResponseWriter, r *http.Request) {
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
	if r.ContentLength < 0 {
		respond(w, 0, nil, domain.ValidationError{Field: "Content-Length", Reason: "an artifact upload states its size"})
		return
	}
	if r.ContentLength > domain.MaxArtifactBytes {
		respond(w, 0, nil, &http.MaxBytesError{Limit: domain.MaxArtifactBytes})
		return
	}
	artifact := domain.Artifact{
		Name: r.PathValue("name"), MediaType: r.Header.Get("Content-Type"), Size: r.ContentLength, SHA256: r.Header.Get(nodeproto.ArtifactSHA256Header),
		Reference: r.Header.Get(nodeproto.ArtifactReferenceHeader), Description: r.Header.Get(nodeproto.ArtifactDescriptionHeader),
	}
	body := http.MaxBytesReader(w, r.Body, domain.MaxArtifactBytes+1)
	stored, err := application.NewUploadArtifact(s.service.Dependencies()).Execute(r.Context(), id, executionID, artifact, body)
	respond(w, http.StatusCreated, stored, err)
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

// renewCertificate trades the certificate the request came with for a
// new one. The certificate is the proof, so only a request over TLS may
// renew: over the socket, where the path is trusted, there is none.
func (s *Server) renewCertificate(w http.ResponseWriter, r *http.Request) {
	identity, ok := identityOf(r)
	if !ok {
		respond(w, 0, nil, domain.ForbiddenError{Reason: "a certificate is renewed over TLS with the certificate itself"})
		return
	}
	var request nodeproto.RenewRequest
	if !readJSON(w, r, &request) {
		return
	}
	result, err := application.NewRenewNodeCertificate(s.service.Dependencies()).Execute(r.Context(), application.CertificateRenewal{NodeID: identity.ID, Serial: identity.Serial, CSRPEM: []byte(request.CSR)})
	if err != nil {
		respond(w, 0, nil, err)
		return
	}
	respond(w, http.StatusOK, nodeproto.EnrollResponse{Node: result.Node.Snapshot(), Certificate: string(result.CertificatePEM), CA: string(result.CAPEM), Serial: result.Serial, NotAfter: result.NotAfter}, nil)
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
	return readJSONLimit(w, r, into, maxBodyBytes)
}

// readJSONLimit is readJSON with its own bound on the body; a body past
// it is 413, not a malformed one.
func readJSONLimit(w http.ResponseWriter, r *http.Request, into any, limit int64) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, limit))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(into); err != nil {
		if tooLarge := new(*http.MaxBytesError); errors.As(err, tooLarge) {
			respond(w, 0, nil, err)
			return false
		}
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
	writeDeadline(w, responseWriteTimeout)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

// writeDeadline gives the answer about to be written d to go out; a
// writer that cannot take a deadline (a test recorder) goes without.
func writeDeadline(w http.ResponseWriter, d time.Duration) {
	_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(d))
}
