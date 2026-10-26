package unixapi

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
	"github.com/underpass-ai/axlr-bots/nodeproto"
)

// Client is the Service, and the node protocol's transport, over the
// control plane's API: a Unix socket (a path, or unix:///path), or an
// https:// address with the TLS configuration of an enrolled node
// (NewTLSClient).
type Client struct {
	http    *http.Client
	base    string
	address string
	token   string
}

// WithToken returns the client sending the operator bearer token on
// every request: what the operator address requires.
func (c *Client) WithToken(token string) *Client {
	copied := *c
	copied.token = token
	return &copied
}

// NewClient talks to the control plane at the address. Runs can take as
// long as a task, so the client sets no overall timeout; the caller's
// context bounds a call. An http(s):// address gets the default
// transport: for the node's certificate use NewTLSClient.
func NewClient(address string) *Client {
	if strings.HasPrefix(address, "http://") || strings.HasPrefix(address, "https://") {
		return &Client{http: &http.Client{}, base: strings.TrimRight(address, "/"), address: address}
	}
	socketPath := strings.TrimPrefix(address, "unix://")
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var dialer net.Dialer
			return dialer.DialContext(ctx, "unix", socketPath)
		},
	}
	return &Client{http: &http.Client{Transport: transport}, base: "http://axlr-bots", address: socketPath}
}

// NewTLSClient talks to the control plane at an https:// address with
// the TLS configuration: the node's certificate and the fleet CA
// (pki.Credentials.ClientTLSConfig), or the CA alone to enroll
// (pki.EnrollTLSConfig).
func NewTLSClient(address string, config *tls.Config) *Client {
	transport := &http.Transport{TLSClientConfig: config, ForceAttemptHTTP2: true}
	return &Client{http: &http.Client{Transport: transport}, base: strings.TrimRight(address, "/"), address: address}
}

// Reachable reports whether a daemon answers on the socket.
func Reachable(ctx context.Context, socketPath string) bool {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	var health map[string]string
	return NewClient(socketPath).call(ctx, http.MethodGet, "/healthz", nil, &health) == nil
}

func (c *Client) RegisterBot(ctx context.Context, definition application.BotDefinition) (domain.BotSnapshot, error) {
	var bot domain.BotSnapshot
	return bot, c.call(ctx, http.MethodPost, "/v1/bots", definition, &bot)
}

func (c *Client) EnableBot(ctx context.Context, id string) (domain.BotSnapshot, error) {
	var bot domain.BotSnapshot
	return bot, c.call(ctx, http.MethodPost, "/v1/bots/"+url.PathEscape(id)+"/enable", nil, &bot)
}

func (c *Client) PauseBot(ctx context.Context, id string) (domain.BotSnapshot, error) {
	var bot domain.BotSnapshot
	return bot, c.call(ctx, http.MethodPost, "/v1/bots/"+url.PathEscape(id)+"/pause", nil, &bot)
}

func (c *Client) UpdateBot(ctx context.Context, id string, patch application.BotPatch) (domain.BotSnapshot, error) {
	var bot domain.BotSnapshot
	return bot, c.call(ctx, http.MethodPatch, "/v1/bots/"+url.PathEscape(id), patch, &bot)
}

func (c *Client) ListEvents(ctx context.Context, id string, limit int) ([]domain.Event, error) {
	events := []domain.Event{}
	path := "/v1/bots/" + url.PathEscape(id) + "/events"
	if limit > 0 {
		path += "?limit=" + strconv.Itoa(limit)
	}
	return events, c.call(ctx, http.MethodGet, path, nil, &events)
}

func (c *Client) StopBot(ctx context.Context, id string) (StopResponse, error) {
	var response StopResponse
	return response, c.call(ctx, http.MethodPost, "/v1/bots/"+url.PathEscape(id)+"/stop", nil, &response)
}

func (c *Client) ListBots(ctx context.Context) ([]domain.BotSnapshot, error) {
	bots := []domain.BotSnapshot{}
	return bots, c.call(ctx, http.MethodGet, "/v1/bots", nil, &bots)
}

func (c *Client) GetBot(ctx context.Context, id string) (domain.BotSnapshot, error) {
	var bot domain.BotSnapshot
	return bot, c.call(ctx, http.MethodGet, "/v1/bots/"+url.PathEscape(id), nil, &bot)
}

func (c *Client) ListGroups(ctx context.Context) ([]application.Group, error) {
	groups := []application.Group{}
	return groups, c.call(ctx, http.MethodGet, "/v1/groups", nil, &groups)
}

func (c *Client) GetGroup(ctx context.Context, name, tenant string) (application.Group, error) {
	var group application.Group
	path := "/v1/groups/" + url.PathEscape(name)
	if tenant != "" {
		path += "?tenant=" + url.QueryEscape(tenant)
	}
	return group, c.call(ctx, http.MethodGet, path, nil, &group)
}

func (c *Client) ListGroupEvents(ctx context.Context, query application.GroupEventsQuery) ([]domain.Event, error) {
	events := []domain.Event{}
	values := url.Values{}
	if query.Tenant != "" {
		values.Set("tenant", query.Tenant)
	}
	if query.After != "" {
		values.Set("after", query.After)
	}
	if query.Limit != 0 {
		values.Set("limit", strconv.Itoa(query.Limit))
	}
	path := "/v1/groups/" + url.PathEscape(query.Name) + "/events"
	if encoded := values.Encode(); encoded != "" {
		path += "?" + encoded
	}
	return events, c.call(ctx, http.MethodGet, path, nil, &events)
}

// SubmitTask posts to the bot's tasks with the key in the
// Idempotency-Key header, as any client of the operator API would.
func (c *Client) SubmitTask(ctx context.Context, request SubmitRequest) (SubmitResponse, error) {
	var response SubmitResponse
	body := TaskRequest{Objective: request.Objective, CorrelationID: request.CorrelationID, Priority: request.Priority}
	headers := http.Header{}
	if request.IdempotencyKey != "" {
		headers.Set(IdempotencyKeyHeader, request.IdempotencyKey)
	}
	return response, c.callWith(ctx, http.MethodPost, "/v1/bots/"+url.PathEscape(request.BotID)+"/tasks", headers, body, &response)
}

// FireTrigger posts the event to the trigger's route with the delivery
// id in the Idempotency-Key header.
func (c *Client) FireTrigger(ctx context.Context, request FireRequest) (FireResponse, error) {
	var response FireResponse
	headers := http.Header{}
	if request.DeliveryID != "" {
		headers.Set(IdempotencyKeyHeader, request.DeliveryID)
	}
	return response, c.callWith(ctx, http.MethodPost, "/v1/triggers/"+url.PathEscape(request.BotID)+"/"+url.PathEscape(request.Trigger), headers, request.Body, &response)
}

func (c *Client) CancelTask(ctx context.Context, id string) (CancelResponse, error) {
	var response CancelResponse
	return response, c.call(ctx, http.MethodPost, "/v1/tasks/"+url.PathEscape(id)+"/cancel", nil, &response)
}

func (c *Client) SendTaskMessage(ctx context.Context, id string, request MessageRequest) (application.SentTaskMessage, error) {
	var response application.SentTaskMessage
	return response, c.call(ctx, http.MethodPost, "/v1/tasks/"+url.PathEscape(id)+"/messages", request, &response)
}

func (c *Client) ListExecutions(ctx context.Context, id string) ([]domain.ExecutionSnapshot, error) {
	executions := []domain.ExecutionSnapshot{}
	return executions, c.call(ctx, http.MethodGet, "/v1/tasks/"+url.PathEscape(id)+"/executions", nil, &executions)
}

func (c *Client) RunTask(ctx context.Context, id string) (RunResponse, error) {
	var response RunResponse
	return response, c.call(ctx, http.MethodPost, "/v1/tasks/"+url.PathEscape(id)+"/run", nil, &response)
}

func (c *Client) DecideTask(ctx context.Context, id string, request DecideRequest) (RunResponse, error) {
	var response RunResponse
	return response, c.call(ctx, http.MethodPost, "/v1/tasks/"+url.PathEscape(id)+"/decide", request, &response)
}

func (c *Client) GetTask(ctx context.Context, id string) (domain.TaskSnapshot, error) {
	var task domain.TaskSnapshot
	return task, c.call(ctx, http.MethodGet, "/v1/tasks/"+url.PathEscape(id), nil, &task)
}

// ListTasks asks for the tasks the query selects, newest first.
func (c *Client) ListTasks(ctx context.Context, query application.TaskQuery) ([]domain.TaskSnapshot, error) {
	tasks := []domain.TaskSnapshot{}
	values := url.Values{}
	if query.BotID != "" {
		values.Set("bot", query.BotID)
	}
	if query.Status != "" {
		values.Set("status", query.Status)
	}
	if !query.Since.IsZero() {
		values.Set("since", query.Since.Format(time.RFC3339Nano))
	}
	if query.Limit != 0 {
		values.Set("limit", strconv.Itoa(query.Limit))
	}
	path := "/v1/tasks"
	if len(values) > 0 {
		path += "?" + values.Encode()
	}
	return tasks, c.call(ctx, http.MethodGet, path, nil, &tasks)
}

// ListTaskEvents asks for the task's timeline, oldest first.
func (c *Client) ListTaskEvents(ctx context.Context, id string, limit int) ([]domain.Event, error) {
	events := []domain.Event{}
	path := "/v1/tasks/" + url.PathEscape(id) + "/events"
	if limit > 0 {
		path += "?limit=" + strconv.Itoa(limit)
	}
	return events, c.call(ctx, http.MethodGet, path, nil, &events)
}

// GetOverview asks for the dashboard's counts since the moment (the
// start of the control plane's day when it is zero).
func (c *Client) GetOverview(ctx context.Context, since time.Time) (application.Overview, error) {
	var overview application.Overview
	path := "/v1/overview"
	if !since.IsZero() {
		path += "?since=" + url.QueryEscape(since.Format(time.RFC3339Nano))
	}
	return overview, c.call(ctx, http.MethodGet, path, nil, &overview)
}

func (c *Client) GetReport(ctx context.Context, id string) (ReportResponse, error) {
	var report ReportResponse
	return report, c.call(ctx, http.MethodGet, "/v1/tasks/"+url.PathEscape(id)+"/report", nil, &report)
}

func (c *Client) GetExecution(ctx context.Context, id string) (domain.ExecutionSnapshot, error) {
	var execution domain.ExecutionSnapshot
	return execution, c.call(ctx, http.MethodGet, "/v1/executions/"+url.PathEscape(id), nil, &execution)
}

// GetArtifact downloads an artifact the control plane keeps; the
// listing is read from the answer's headers.
func (c *Client) GetArtifact(ctx context.Context, execution, name string) (ArtifactContent, error) {
	request, err := c.request(ctx, http.MethodGet, "/v1/executions/"+url.PathEscape(execution)+"/artifacts/"+url.PathEscape(name), nil)
	if err != nil {
		return ArtifactContent{}, err
	}
	response, err := c.http.Do(request)
	if err != nil {
		return ArtifactContent{}, fmt.Errorf("control plane at %s: %w", c.address, err)
	}
	if response.StatusCode >= 400 {
		defer response.Body.Close()
		raw, _ := io.ReadAll(io.LimitReader(response.Body, 1<<20))
		var failure ErrorResponse
		_ = json.Unmarshal(raw, &failure)
		return ArtifactContent{}, fromWire(response.StatusCode, failure)
	}
	artifact := domain.Artifact{Name: name, MediaType: response.Header.Get("Content-Type"), Size: response.ContentLength, SHA256: response.Header.Get(nodeproto.ArtifactSHA256Header), Stored: true}
	return ArtifactContent{Artifact: artifact, Body: response.Body}, nil
}

func (c *Client) ListNodes(ctx context.Context) ([]domain.NodeSnapshot, error) {
	nodes := []domain.NodeSnapshot{}
	return nodes, c.call(ctx, http.MethodGet, "/v1/nodes", nil, &nodes)
}

func (c *Client) GetNode(ctx context.Context, id string) (domain.NodeSnapshot, error) {
	var node domain.NodeSnapshot
	return node, c.call(ctx, http.MethodGet, "/v1/nodes/"+url.PathEscape(id), nil, &node)
}

func (c *Client) CreateEnrollmentToken(ctx context.Context, request EnrollTokenRequest) (EnrollTokenResponse, error) {
	var response EnrollTokenResponse
	return response, c.call(ctx, http.MethodPost, "/v1/enrollments", request, &response)
}

func (c *Client) RevokeNode(ctx context.Context, id string) (RevokeResponse, error) {
	var response RevokeResponse
	return response, c.call(ctx, http.MethodPost, "/v1/nodes/"+url.PathEscape(id)+"/revoke", nil, &response)
}

// Enroll presents a token and a signing request and receives the node's
// certificate: the one call a node makes before it has one.
func (c *Client) Enroll(ctx context.Context, request nodeproto.EnrollRequest) (nodeproto.EnrollResponse, error) {
	var response nodeproto.EnrollResponse
	return response, c.call(ctx, http.MethodPost, "/v1/enroll", request, &response)
}

// Register is the node protocol's registration (application.NodeTransport).
func (c *Client) Register(ctx context.Context, registration application.NodeRegistration) (domain.NodeSnapshot, error) {
	var node domain.NodeSnapshot
	request := nodeproto.RegisterRequest{ID: registration.ID, HostName: registration.HostName, Labels: registration.Labels, Capabilities: registration.Capabilities}
	return node, c.call(ctx, http.MethodPost, "/v1/nodes", request, &node)
}

// Heartbeat tells the control plane the node is alive.
func (c *Client) Heartbeat(ctx context.Context, node domain.NodeID) (domain.NodeSnapshot, error) {
	var snapshot domain.NodeSnapshot
	return snapshot, c.call(ctx, http.MethodPost, "/v1/nodes/"+url.PathEscape(node.String())+"/heartbeat", nil, &snapshot)
}

// NextAssignment long-polls for the node's next execution.
func (c *Client) NextAssignment(ctx context.Context, node domain.NodeID, wait time.Duration) (application.Assignment, bool, error) {
	var message nodeproto.AssignmentMessage
	path := "/v1/nodes/" + url.PathEscape(node.String()) + "/assignments/next?wait=" + url.QueryEscape(wait.String())
	found, err := c.callMaybe(ctx, http.MethodGet, path, nil, &message)
	if err != nil || !found {
		return application.Assignment{}, false, err
	}
	assignment, err := nodeproto.ToAssignment(message)
	if err != nil {
		return application.Assignment{}, false, fmt.Errorf("control plane sent a bad assignment: %w", err)
	}
	return assignment, true, nil
}

// RenewLease extends the node's lease on an execution. A refusal of the
// lease itself (the execution has ended, or is another node's: a state
// error) is application.ErrLeaseLost, so the node stops the run.
func (c *Client) RenewLease(ctx context.Context, node domain.NodeID, execution domain.ExecutionID) (time.Time, error) {
	var lease nodeproto.LeaseMessage
	err := c.call(ctx, http.MethodPost, "/v1/nodes/"+url.PathEscape(node.String())+"/executions/"+url.PathEscape(execution.String())+"/lease", nil, &lease)
	var remote RemoteError
	if errors.As(err, &remote) && remote.Kind == KindState {
		return time.Time{}, fmt.Errorf("%w: %s", application.ErrLeaseLost, remote.Message)
	}
	return lease.LeaseUntil, err
}

// UploadArtifact puts an artifact's bytes to the control plane, before
// the report: the size goes as the Content-Length, the hash in its
// header, and the control plane checks both.
func (c *Client) UploadArtifact(ctx context.Context, node domain.NodeID, execution domain.ExecutionID, artifact domain.Artifact, content io.Reader) error {
	path := "/v1/nodes/" + url.PathEscape(node.String()) + "/executions/" + url.PathEscape(execution.String()) + "/artifacts/" + url.PathEscape(artifact.Name)
	request, err := c.request(ctx, http.MethodPut, path, content)
	if err != nil {
		return err
	}
	request.ContentLength = artifact.Size
	if artifact.Size == 0 {
		request.Body = http.NoBody
	}
	if artifact.MediaType != "" {
		request.Header.Set("Content-Type", artifact.MediaType)
	}
	request.Header.Set(nodeproto.ArtifactSHA256Header, artifact.SHA256)
	if artifact.Reference != "" {
		request.Header.Set(nodeproto.ArtifactReferenceHeader, artifact.Reference)
	}
	if description := headerSafe(artifact.Description); description != "" {
		request.Header.Set(nodeproto.ArtifactDescriptionHeader, description)
	}
	response, err := c.http.Do(request)
	if err != nil {
		return fmt.Errorf("control plane at %s: %w", c.address, err)
	}
	defer response.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if response.StatusCode >= 400 {
		var failure ErrorResponse
		_ = json.Unmarshal(raw, &failure)
		return fromWire(response.StatusCode, failure)
	}
	return nil
}

// headerSafe keeps the printable ASCII of a description, bounded, so it
// fits in a header.
func headerSafe(text string) string {
	var b strings.Builder
	for _, r := range text {
		if r >= 0x20 && r < 0x7f && b.Len() < 300 {
			b.WriteRune(r)
		}
	}
	return strings.TrimSpace(b.String())
}

// Report hands the execution's result to the control plane.
func (c *Client) Report(ctx context.Context, node domain.NodeID, execution domain.ExecutionID, report application.ExecutionReport) (application.ReportAck, error) {
	var ack nodeproto.ReportAck
	err := c.call(ctx, http.MethodPost, "/v1/nodes/"+url.PathEscape(node.String())+"/executions/"+url.PathEscape(execution.String())+"/report", nodeproto.FromReport(report), &ack)
	if err != nil {
		return application.ReportAck{}, err
	}
	outcome, err := domain.ParseExecutionOutcome(ack.Outcome)
	if err != nil {
		return application.ReportAck{}, err
	}
	return application.ReportAck{Accepted: ack.Accepted, Outcome: outcome}, nil
}

// Delegate asks the control plane that another bot do a task for the
// execution (application.NodeTransport); a refusal comes back as the
// answer's Refused.
func (c *Client) Delegate(ctx context.Context, node domain.NodeID, execution domain.ExecutionID, command application.DelegateCommand) (application.DelegationAnswer, error) {
	var message nodeproto.DelegationMessage
	path := "/v1/nodes/" + url.PathEscape(node.String()) + "/executions/" + url.PathEscape(execution.String()) + "/delegations"
	request := nodeproto.DelegateMessage{Bot: command.Bot, Objective: command.Objective, Wait: command.Wait}
	if err := c.call(ctx, http.MethodPost, path, request, &message); err != nil {
		return application.DelegationAnswer{}, err
	}
	return nodeproto.ToDelegation(message)
}

// Emit tells the execution's teams an event
// (application.NodeTransport); a refusal comes back as the answer's
// Refused.
func (c *Client) Emit(ctx context.Context, node domain.NodeID, execution domain.ExecutionID, command application.EmitEventCommand) (application.EmitAnswer, error) {
	var message nodeproto.EmittedMessage
	path := "/v1/nodes/" + url.PathEscape(node.String()) + "/executions/" + url.PathEscape(execution.String()) + "/events"
	request := nodeproto.EmitMessage{Name: command.Name, Message: command.Message, Severity: command.Severity, Data: command.Data, To: command.To}
	if err := c.call(ctx, http.MethodPost, path, request, &message); err != nil {
		return application.EmitAnswer{}, err
	}
	return nodeproto.ToEmitted(message), nil
}

// AwaitDelegation waits up to wait for the child to end and answers its
// state.
func (c *Client) AwaitDelegation(ctx context.Context, node domain.NodeID, execution domain.ExecutionID, child domain.TaskID, wait time.Duration) (application.DelegationAnswer, error) {
	var message nodeproto.DelegationMessage
	path := "/v1/nodes/" + url.PathEscape(node.String()) + "/executions/" + url.PathEscape(execution.String()) + "/delegations/" + url.PathEscape(child.String()) + "?wait=" + url.QueryEscape(wait.String())
	if err := c.call(ctx, http.MethodGet, path, nil, &message); err != nil {
		return application.DelegationAnswer{}, err
	}
	return nodeproto.ToDelegation(message)
}

// ExecutionMessages reads the operator's messages for an execution the
// node runs, after the one named by after.
func (c *Client) ExecutionMessages(ctx context.Context, node domain.NodeID, execution domain.ExecutionID, after string) ([]application.RelayedMessage, error) {
	var message nodeproto.MessagesMessage
	path := "/v1/nodes/" + url.PathEscape(node.String()) + "/executions/" + url.PathEscape(execution.String()) + "/messages?after=" + url.QueryEscape(after)
	if err := c.call(ctx, http.MethodGet, path, nil, &message); err != nil {
		return nil, err
	}
	return message.Messages, nil
}

// request builds a request to the control plane with the bearer token.
func (c *Client) request(ctx context.Context, method, path string, body io.Reader) (*http.Request, error) {
	request, err := http.NewRequestWithContext(ctx, method, c.base+path, body)
	if err != nil {
		return nil, err
	}
	if c.token != "" {
		request.Header.Set("Authorization", "Bearer "+c.token)
	}
	return request, nil
}

// callMaybe is call for an answer that may be 204 No Content: found is
// false then.
func (c *Client) callMaybe(ctx context.Context, method, path string, body, into any) (bool, error) {
	status, raw, err := c.do(ctx, method, path, body)
	if err != nil {
		return false, err
	}
	if status == http.StatusNoContent {
		return false, nil
	}
	if status >= 400 {
		var failure ErrorResponse
		_ = json.Unmarshal(raw, &failure)
		return false, fromWire(status, failure)
	}
	if err := json.Unmarshal(raw, into); err != nil {
		return false, fmt.Errorf("control plane answered %s: %w", method+" "+path, err)
	}
	return true, nil
}

// call sends one request and decodes the answer, or the error it carries.
func (c *Client) call(ctx context.Context, method, path string, body, into any) error {
	return c.callWith(ctx, method, path, nil, body, into)
}

// callWith is call with extra headers.
func (c *Client) callWith(ctx context.Context, method, path string, headers http.Header, body, into any) error {
	status, raw, err := c.doWith(ctx, method, path, headers, body)
	if err != nil {
		return err
	}
	if status >= 400 {
		var failure ErrorResponse
		_ = json.Unmarshal(raw, &failure)
		return fromWire(status, failure)
	}
	if into == nil {
		return nil
	}
	if err := json.Unmarshal(raw, into); err != nil {
		return fmt.Errorf("control plane answered %s: %w", method+" "+path, err)
	}
	return nil
}

// do sends one request and returns the status and the body.
func (c *Client) do(ctx context.Context, method, path string, body any) (int, []byte, error) {
	return c.doWith(ctx, method, path, nil, body)
}

// doWith is do with extra headers (the bearer token, an idempotency
// key). A json.RawMessage body is sent as it is.
func (c *Client) doWith(ctx context.Context, method, path string, headers http.Header, body any) (int, []byte, error) {
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return 0, nil, err
		}
		reader = bytes.NewReader(raw)
	}
	request, err := c.request(ctx, method, path, reader)
	if err != nil {
		return 0, nil, err
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	for key, values := range headers {
		for _, value := range values {
			request.Header.Add(key, value)
		}
	}
	response, err := c.http.Do(request)
	if err != nil {
		return 0, nil, fmt.Errorf("control plane at %s: %w", c.address, err)
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, 16<<20))
	if err != nil {
		return 0, nil, err
	}
	return response.StatusCode, raw, nil
}
