package githubhooks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"slices"
	"time"

	"github.com/underpass-ai/axlr-bots/application"
)

// Path is the channel the handler serves: the spec's githubWebhook.
const Path = "/github"

// MaxBody is the largest delivery the handler reads; a larger one is 413.
const MaxBody = 1 << 20

// DefaultPublishTimeout bounds the publishes of one delivery, under the
// ten seconds GitHub waits for an answer.
const DefaultPublishTimeout = 8 * time.Second

// Config is what a Handler needs.
type Config struct {
	// Secret is the hook's secret; empty is refused.
	Secret []byte
	// Team is the team the events are published to (dev).
	Team string
	// Repos is the allow-list of owner/name; empty is DefaultRepos.
	Repos []string
	// Publisher publishes as the operator.
	Publisher Publisher
	// Log gets one line per delivery, never its body; nil discards.
	Log *log.Logger
	// PublishTimeout bounds the publishes of one delivery; zero is
	// DefaultPublishTimeout.
	PublishTimeout time.Duration
}

// Handler is POST /github: it verifies a delivery, maps what it uses and
// publishes it to the team.
type Handler struct {
	secret  []byte
	team    string
	repos   []string
	publish Publisher
	log     *log.Logger
	timeout time.Duration
}

// NewHandler checks the configuration and wires the handler.
func NewHandler(config Config) (*Handler, error) {
	if len(config.Secret) == 0 {
		return nil, errors.New("githubhooks: the hook's secret is empty")
	}
	if config.Team == "" {
		return nil, errors.New("githubhooks: no team to publish to")
	}
	if config.Publisher == nil {
		return nil, errors.New("githubhooks: no publisher")
	}
	repos := slices.Clone(config.Repos)
	if len(repos) == 0 {
		repos = slices.Clone(DefaultRepos)
	}
	logger := config.Log
	if logger == nil {
		logger = log.New(io.Discard, "", 0)
	}
	timeout := config.PublishTimeout
	if timeout <= 0 {
		timeout = DefaultPublishTimeout
	}
	return &Handler{secret: slices.Clone(config.Secret), team: config.Team, repos: repos, publish: config.Publisher, log: logger, timeout: timeout}, nil
}

// ServeHTTP answers one delivery: 405, 413, 401, 400, 204 for what it
// does not use, 202 once published, 502 when a publish failed.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != Path {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		answer(w, http.StatusMethodNotAllowed, "POST only")
		return
	}
	event, delivery := r.Header.Get(HeaderEvent), r.Header.Get(HeaderDelivery)
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, MaxBody))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			h.log.Printf("github-hooks: delivery %q (%s): refused, body over %d bytes", delivery, event, MaxBody)
			answer(w, http.StatusRequestEntityTooLarge, "body too large")
			return
		}
		answer(w, http.StatusBadRequest, "unreadable body")
		return
	}
	if !Verify(h.secret, body, r.Header.Get(HeaderSignature)) {
		reason := "bad signature"
		if r.Header.Get(HeaderSignature) == "" {
			reason = "no signature"
		}
		h.log.Printf("github-hooks: delivery %q (%s) from %s: refused, %s", delivery, event, r.RemoteAddr, reason)
		answer(w, http.StatusUnauthorized, reason)
		return
	}
	if event != GitHubCheckSuite && event != GitHubPullRequest {
		if event != GitHubPing {
			h.log.Printf("github-hooks: delivery %q: event %q ignored", delivery, event)
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if !deliveryPattern.MatchString(delivery) {
		h.log.Printf("github-hooks: delivery %q (%s): refused, no valid %s", delivery, event, HeaderDelivery)
		answer(w, http.StatusBadRequest, "no valid "+HeaderDelivery)
		return
	}
	commands, repo, err := h.commands(event, delivery, body)
	if err != nil {
		h.log.Printf("github-hooks: delivery %s (%s): refused, %v", delivery, event, err)
		answer(w, http.StatusBadRequest, "not a JSON payload of "+event)
		return
	}
	if !allowed(h.repos, repo) {
		h.log.Printf("github-hooks: delivery %s (%s): repository %q not in the allow-list, ignored", delivery, event, repo)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if len(commands) == 0 {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), h.timeout)
	defer cancel()
	for _, command := range commands {
		published, err := h.publish.PublishGroupEvent(ctx, command)
		if err != nil {
			h.log.Printf("github-hooks: delivery %s: %s %s not published: %v", delivery, command.Name, command.IdempotencyKey, err)
			answer(w, http.StatusBadGateway, "publish failed; redeliver")
			return
		}
		h.log.Printf("github-hooks: delivery %s: %s %s → %s (new %t)", delivery, command.Name, command.IdempotencyKey, published.EventID, published.Created)
	}
	answer(w, http.StatusAccepted, fmt.Sprintf("published %d", len(commands)))
}

// commands decodes the payload of the event and maps it.
func (h *Handler) commands(event, delivery string, body []byte) ([]application.PublishGroupEventCommand, string, error) {
	if event == GitHubCheckSuite {
		var payload CheckSuiteEvent
		if err := json.Unmarshal(body, &payload); err != nil {
			return nil, "", err
		}
		commands, err := FromCheckSuite(delivery, h.team, payload)
		return commands, payload.Repository.FullName, err
	}
	var payload PullRequestEvent
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, "", err
	}
	commands, err := FromPullRequest(delivery, h.team, payload)
	return commands, payload.Repository.FullName, err
}

func answer(w http.ResponseWriter, status int, text string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, text+"\n")
}
