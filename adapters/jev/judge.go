// Package jev is the node's second runtime beside AXLR: a bot whose every
// decision is one question to Jev, TypeSafe's judgement model, and whose
// steps are fixed. No language model plans the run: the program in the
// bot's definition says which tool starts it, which tool acts on Jev's
// choice and which one waits for the next question; the world being acted
// on (an MCP server) asks the questions. The control plane's contract is
// AXLR's: one execution in, one report out, the budgets and the policy
// kept, a receipt of every decision.
package jev

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Endpoint is TypeSafe's judgement API.
const Endpoint = "https://api.typesafe.ai/v1/systemone"

// DefaultModel is the Jev version AXLR and KMP pin; never a -latest alias.
const DefaultModel = "jev-1.13.0"

// InputPricePerToken is TypeSafe's price of an input token for Jev
// (0.042 USD per million, docs.typesafe.ai/models, 30 Oct 2026); output
// is free.
const InputPricePerToken = 0.042 / 1e6

// Limits keep one question inside one TypeSafe request, as AXLR's
// axlr_judge does.
const (
	MaxStateBytes    = 24 << 10
	MaxQuestionBytes = 2000
	MaxOptions       = 16
	MaxOptionBytes   = 200
	maxBodyBytes     = 256 << 10
	maxRetries       = 2
	maxRetryWait     = 5 * time.Second
	questionKey      = "q"
)

var pinnedModel = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)

// Question is one choice put to Jev: the state, the question, the options.
type Question struct {
	State    string   `json:"state"`
	Question string   `json:"question"`
	Options  []string `json:"options"`
}

// Validate refuses a question Jev cannot answer as a choice.
func (q Question) Validate() error {
	switch {
	case strings.TrimSpace(q.State) == "":
		return errors.New("the ask has no state")
	case len(q.State) > MaxStateBytes:
		return fmt.Errorf("the ask's state is over %d bytes", MaxStateBytes)
	case strings.TrimSpace(q.Question) == "":
		return errors.New("the ask has no question")
	case len(q.Question) > MaxQuestionBytes:
		return fmt.Errorf("the ask's question is over %d bytes", MaxQuestionBytes)
	case len(q.Options) < 2 || len(q.Options) > MaxOptions:
		return fmt.Errorf("the ask offers %d options; Jev chooses among 2 to %d", len(q.Options), MaxOptions)
	}
	seen := map[string]bool{}
	for _, option := range q.Options {
		if strings.TrimSpace(option) == "" || len(option) > MaxOptionBytes {
			return fmt.Errorf("an option is empty or over %d bytes", MaxOptionBytes)
		}
		if seen[option] {
			return fmt.Errorf("the option %q is offered twice", option)
		}
		seen[option] = true
	}
	return nil
}

// Verdict is Jev's answer: the option chosen, the probability it gave
// that option, its confidence, and what the call cost.
type Verdict struct {
	Model       string
	Choice      string
	Probability float64
	Confidence  float64
	InputTokens int
	Took        time.Duration
}

// Judge asks Jev one question.
type Judge interface {
	Choose(ctx context.Context, question Question) (Verdict, error)
}

// ClientConfig sets up the TypeSafe client. Endpoint and HTTPClient are
// for tests; empty means TypeSafe's API and a client without redirects.
type ClientConfig struct {
	APIKey     string
	Model      string
	Timeout    time.Duration
	Endpoint   string
	HTTPClient *http.Client
}

// Client is TypeSafe's API, as AXLR's adapter calls it: a pinned model, a
// bounded body, no redirects, at most two retries on 429. The key goes in
// the Authorization header and nowhere else.
type Client struct {
	key, model, endpoint string
	http                 http.Client
}

// NewClient checks the key and the model.
func NewClient(config ClientConfig) (*Client, error) {
	key := strings.TrimSpace(config.APIKey)
	if key == "" || strings.ContainsAny(key, "\r\n") {
		return nil, errors.New("TYPESAFE_API_KEY is required for Jev")
	}
	model := config.Model
	if model == "" {
		model = DefaultModel
	}
	if !pinnedModel.MatchString(model) || strings.HasSuffix(model, "-latest") {
		return nil, errors.New("the Jev model must be a pinned version such as jev-1.13.0")
	}
	timeout := config.Timeout
	if timeout == 0 {
		timeout = 20 * time.Second
	}
	endpoint := config.Endpoint
	if endpoint == "" {
		endpoint = Endpoint
	}
	client := http.Client{}
	if config.HTTPClient != nil {
		client = *config.HTTPClient
	}
	client.Timeout = timeout
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &Client{key: key, model: model, endpoint: endpoint, http: client}, nil
}

// Model is the pinned Jev version the client asks.
func (c *Client) Model() string { return c.model }

// Choose asks Jev to choose one of the question's options.
func (c *Client) Choose(ctx context.Context, question Question) (Verdict, error) {
	if err := question.Validate(); err != nil {
		return Verdict{}, err
	}
	criteria := make(map[string]any, len(question.Options))
	for _, option := range question.Options {
		criteria[option] = nil
	}
	body, err := json.Marshal(map[string]any{"state": question.State, "model": c.model, "questions": map[string]any{
		questionKey: map[string]any{"type": "choice", "instructions": question.Question, "criteria": criteria},
	}})
	if err != nil || len(body) > maxBodyBytes {
		return Verdict{}, errors.New("the request for Jev is too large")
	}
	started := time.Now()
	data, err := c.post(ctx, body)
	if err != nil {
		return Verdict{}, err
	}
	verdict, err := c.verdict(data, question)
	verdict.Took = time.Since(started)
	return verdict, err
}

func (c *Client) post(ctx context.Context, body []byte) ([]byte, error) {
	for attempt := 0; ; attempt++ {
		request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
		if err != nil {
			return nil, errors.New("could not create the Jev request")
		}
		request.Header.Set("Authorization", "Bearer "+c.key)
		request.Header.Set("Content-Type", "application/json")
		response, err := c.http.Do(request)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, errors.New("TypeSafe did not answer")
		}
		data, readErr := io.ReadAll(io.LimitReader(response.Body, maxBodyBytes+1))
		_ = response.Body.Close()
		switch {
		case response.StatusCode == http.StatusTooManyRequests && attempt < maxRetries:
			wait := time.Second
			if seconds, err := strconv.Atoi(response.Header.Get("Retry-After")); err == nil && seconds >= 0 {
				wait = min(time.Duration(seconds)*time.Second, maxRetryWait)
			}
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(wait):
			}
			continue
		case response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden:
			return nil, fmt.Errorf("TypeSafe refused the API key (HTTP %d)", response.StatusCode)
		case response.StatusCode < 200 || response.StatusCode >= 300:
			return nil, fmt.Errorf("TypeSafe answered HTTP %d", response.StatusCode)
		case readErr != nil:
			return nil, errors.New("TypeSafe did not answer")
		case len(data) > maxBodyBytes:
			return nil, errors.New("the answer from TypeSafe is over 256 KiB")
		}
		return data, nil
	}
}

func (c *Client) verdict(data []byte, question Question) (Verdict, error) {
	var wire struct {
		Model   string                     `json:"model"`
		Answers map[string]json.RawMessage `json:"answers"`
		Usage   struct {
			InputTokens int `json:"input_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return Verdict{}, errors.New("the answer from TypeSafe is not JSON")
	}
	if wire.Model != c.model {
		return Verdict{}, errors.New("TypeSafe answered with another model than the one asked")
	}
	raw, ok := wire.Answers[questionKey]
	if !ok || len(wire.Answers) != 1 {
		return Verdict{}, errors.New("TypeSafe's answers do not match the question sent")
	}
	var answer struct {
		Type          string              `json:"type"`
		Choice        string              `json:"choice"`
		Probabilities map[string]*float64 `json:"probabilities"`
		Confidence    *float64            `json:"confidence"`
	}
	if err := json.Unmarshal(raw, &answer); err != nil {
		return Verdict{}, errors.New("TypeSafe's answer is not a choice")
	}
	if answer.Type != "choice" || !probability(answer.Confidence) || len(answer.Probabilities) != len(question.Options) {
		return Verdict{}, errors.New("TypeSafe's answer is not a choice among the options")
	}
	// A null probability is no probability: decoded into a float64 it
	// would read as 0 and pass for an answer.
	chosen := -1.0
	for _, option := range question.Options {
		p, ok := answer.Probabilities[option]
		if !ok || !probability(p) {
			return Verdict{}, errors.New("TypeSafe's probabilities do not cover the options offered")
		}
		if option == answer.Choice {
			chosen = *p
		}
	}
	if chosen < 0 {
		return Verdict{}, errors.New("TypeSafe chose an option that was not offered")
	}
	return Verdict{Model: wire.Model, Choice: answer.Choice, Probability: chosen, Confidence: *answer.Confidence, InputTokens: max(wire.Usage.InputTokens, 0)}, nil
}

func probability(p *float64) bool {
	return p != nil && !math.IsNaN(*p) && *p >= 0 && *p <= 1
}
