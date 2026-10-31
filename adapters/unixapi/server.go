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

// groupPollInterval is how often a waiting read of a team's timeline
// looks again: each look reads the event log.
const groupPollInterval = time.Second

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
	mux.HandleFunc("POST /v1/bots", s.registerBot)
	mux.HandleFunc("GET /v1/bots", s.listBots)
	mux.HandleFunc("GET /v1/bots/{id}", s.getBot)
	mux.HandleFunc("PATCH /v1/bots/{id}", s.patchBot)
	mux.HandleFunc("POST /v1/bots/{id}/enable", s.enableBot)
	mux.HandleFunc("POST /v1/bots/{id}/pause", s.pauseBot)
	mux.HandleFunc("POST /v1/bots/{id}/stop", s.stopBot)
	mux.HandleFunc("POST /v1/bots/{id}/tasks", s.submitBotTask)
	mux.HandleFunc("GET /v1/bots/{id}/events", s.listEvents)
	mux.HandleFunc("GET /v1/groups", s.listGroups)
	mux.HandleFunc("GET /v1/groups/{name}", s.getGroup)
	mux.HandleFunc("GET /v1/groups/{name}/events", s.listGroupEvents)
	mux.HandleFunc("POST /v1/groups/{name}/events", s.publishGroupEvent)
	mux.HandleFunc("GET /v1/groups/{name}/topics", s.listGroupTopics)
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
	mux.HandleFunc("GET /v1/nodes/{id}/assignments/next", s.authenticated(s.nextAssignment))
	mux.HandleFunc("POST /v1/nodes/{id}/executions/{exec}/lease", s.authenticated(s.renewLease))
	mux.HandleFunc("PUT /v1/nodes/{id}/executions/{exec}/artifacts/{name}", s.authenticated(s.uploadArtifact))
	mux.HandleFunc("POST /v1/nodes/{id}/executions/{exec}/report", s.authenticated(s.report))
	mux.HandleFunc("POST /v1/nodes/{id}/executions/{exec}/delegations", s.authenticated(s.delegate))
	mux.HandleFunc("GET /v1/nodes/{id}/executions/{exec}/delegations/{task}", s.authenticated(s.awaitDelegation))
	mux.HandleFunc("POST /v1/nodes/{id}/executions/{exec}/events", s.authenticated(s.emit))
	mux.HandleFunc("POST /v1/nodes/{id}/executions/{exec}/subscriptions", s.authenticated(s.subscribe))
	mux.HandleFunc("GET /v1/nodes/{id}/executions/{exec}/messages", s.authenticated(s.executionMessages))
	mux.HandleFunc("POST /v1/nodes/{id}/executions/{exec}/said", s.authenticated(s.said))
}

// ServeTLS serves the node protocol on the listener with the TLS
// configuration (pki.ServerTLSConfig) until ctx ends. The caller binds
// the listener, so it knows the address when it asked for port 0.
func (s *Server) ServeTLS(ctx context.Context, listener net.Listener, config *tls.Config) error {
	server := &http.Server{Handler: s.NodeHandler(), TLSConfig: config, ReadHeaderTimeout: 10 * time.Second, ErrorLog: log.New(io.Discard, "", 0)}
	return s.run(ctx, server, func() error { return server.ServeTLS(listener, "", "") }, func() {})
}

// ServeAPI serves the operator routes on the listener over TLS behind
// the bearer token until ctx ends.
func (s *Server) ServeAPI(ctx context.Context, listener net.Listener, config *tls.Config, token string) error {
	if token == "" {
		return errors.New("unixapi: the operator address needs a bearer token")
	}
	server := &http.Server{Handler: s.OperatorHandler(token), TLSConfig: config, ReadHeaderTimeout: 10 * time.Second, ErrorLog: log.New(io.Discard, "", 0)}
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
	wait := time.Duration(0)
	if raw := r.URL.Query().Get("wait"); raw != "" {
		parsed, err := time.ParseDuration(raw)
		if err != nil || parsed < 0 {
			respond(w, 0, nil, domain.ValidationError{Field: "wait", Reason: "must be a duration"})
			return
		}
		wait = min(parsed, maxPollWait)
	}
	query := application.GroupEventsQuery{Name: r.PathValue("name"), Tenant: r.URL.Query().Get("tenant"), After: r.URL.Query().Get("after"), Limit: limit}
	deadline := time.Now().Add(wait)
	for {
		events, err := s.service.ListGroupEvents(r.Context(), query)
		if err != nil || len(events) > 0 || !time.Now().Before(deadline) {
			respond(w, http.StatusOK, events, err)
			return
		}
		select {
		case <-r.Context().Done():
			respond(w, http.StatusOK, events, nil)
			return
		case <-time.After(groupPollInterval):
		}
	}
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

func (s *Server) patchBot(w http.ResponseWriter, r *http.Request) {
	var patch application.BotPatch
	if !readJSON(w, r, &patch) {
		return
	}
	value, err := s.service.UpdateBot(r.Context(), r.PathValue("id"), patch)
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

func (s *Server) listTaskEvents(w http.ResponseWriter, r *http.Request) {
	limit, err := queryLimit(r)
	if err != nil {
		respond(w, 0, nil, err)
		return
	}
	value, err := s.service.ListTaskEvents(r.Context(), r.PathValue("id"), limit)
	respond(w, http.StatusOK, value, err)
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

func (s *Server) cancelTask(w http.ResponseWriter, r *http.Request) {
	value, err := s.service.CancelTask(r.Context(), r.PathValue("id"))
	respond(w, http.StatusOK, value, err)
}

// fireTrigger is POST /v1/triggers/{bot}/{trigger}: the body is the
// event. Over TLS (the operator address) the request must carry the
// trigger's secret or a signature of the body under its signing key;
// over the socket the caller is trusted, as for every other route.
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
	response, err := s.service.FireTrigger(r.Context(), FireRequest{BotID: botID, Trigger: trigger, DeliveryID: r.Header.Get(IdempotencyKeyHeader), Body: body})
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
	value, err := s.service.DecideTask(r.Context(), r.PathValue("id"), request)
	respond(w, http.StatusOK, value, err)
}

// decideAs is POST /v1/tasks/{id}/approve and /deny: the decision is the
// route, the pending call the optional ?call= (what a notification's
// links carry), and no body is read.
func (s *Server) decideAs(decision string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		request := DecideRequest{Decision: decision, CallID: r.URL.Query().Get("call")}
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
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, content.Body)
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
	respond(w, http.StatusOK, nodeproto.ReportAck{Accepted: result.Accepted, Outcome: result.Execution.Outcome().String()}, nil)
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
		answer, err := read.Execute(r.Context(), id, executionID, child)
		if err != nil {
			respond(w, 0, nil, err)
			return
		}
		if answer.Done || time.Now().After(deadline) || r.Context().Err() != nil {
			respond(w, http.StatusOK, nodeproto.FromDelegation(answer), nil)
			return
		}
		select {
		case <-r.Context().Done():
			respond(w, http.StatusOK, nodeproto.FromDelegation(answer), nil)
			return
		case <-time.After(pollInterval):
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

// readJSONLimit is readJSON with its own bound on the body.
func readJSONLimit(w http.ResponseWriter, r *http.Request, into any, limit int64) bool {
	decoder := json.NewDecoder(io.LimitReader(r.Body, limit))
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
