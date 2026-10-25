// Package notify delivers the notifications of approval requests: an
// HTTP POST of the JSON to a webhook, signed with the target's secret,
// or a run of a local program with the JSON on its stdin (a desktop
// notification, a script that relays it elsewhere). Each Send is one
// attempt; the application retries.
package notify

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/underpass-ai/axlr-bots/domain"
)

// Headers of a webhook delivery.
const (
	// DeliveryHeader carries the delivery id, the same on every attempt.
	DeliveryHeader = "X-Axlr-Delivery"
	// EventHeader names what the delivery announces.
	EventHeader = "X-Axlr-Event"
	// SignatureHeader is `sha256=` and the HMAC-SHA256 of the body under
	// the target's secret.
	SignatureHeader = "X-Axlr-Signature"
)

// DefaultTimeout bounds one attempt.
const DefaultTimeout = 10 * time.Second

// Sender is the application's NotificationSender.
type Sender struct {
	// Client posts to webhooks; nil is a client that follows no redirect,
	// so a signed body never goes where the target did not name.
	Client *http.Client
	// Timeout bounds one attempt; zero is DefaultTimeout.
	Timeout time.Duration
}

// NewSender sends with the defaults.
func NewSender() *Sender { return &Sender{} }

// Send makes one attempt at the delivery.
func (s *Sender) Send(ctx context.Context, target domain.NotifyTarget, delivery domain.Delivery) error {
	ctx, cancel := context.WithTimeout(ctx, s.timeout())
	defer cancel()
	switch target.Kind() {
	case domain.NotifyWebhook:
		return s.post(ctx, target, delivery)
	case domain.NotifyCommand:
		return s.run(ctx, target, delivery)
	}
	return fmt.Errorf("notify: target %s has no type a sender knows", target.Name())
}

func (s *Sender) timeout() time.Duration {
	if s.Timeout > 0 {
		return s.Timeout
	}
	return DefaultTimeout
}

func (s *Sender) client() *http.Client {
	if s.Client != nil {
		return s.Client
	}
	return &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

// post sends the payload; any 2xx is a delivery.
func (s *Sender) post(ctx context.Context, target domain.NotifyTarget, delivery domain.Delivery) error {
	body := delivery.Payload()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, target.URL(), bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("notify: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("User-Agent", "axlr-bots")
	request.Header.Set(DeliveryHeader, delivery.ID().String())
	request.Header.Set("Idempotency-Key", delivery.ID().String())
	request.Header.Set(EventHeader, domain.EventTaskWaitingApproval.String())
	request.Header.Set(SignatureHeader, target.Sign(body))
	response, err := s.client().Do(request)
	if err != nil {
		return fmt.Errorf("notify: %w", err)
	}
	defer response.Body.Close()
	answer, _ := io.ReadAll(io.LimitReader(response.Body, 512))
	if response.StatusCode < 200 || response.StatusCode > 299 {
		line := strings.TrimSpace(string(answer))
		if line == "" {
			return fmt.Errorf("HTTP %d", response.StatusCode)
		}
		return fmt.Errorf("HTTP %d: %s", response.StatusCode, firstLine(line))
	}
	return nil
}

// run starts the program with the payload on its stdin and the ids in
// its environment; exit status 0 is a delivery.
func (s *Sender) run(ctx context.Context, target domain.NotifyTarget, delivery domain.Delivery) error {
	command := target.Command()
	cmd := exec.CommandContext(ctx, command[0], command[1:]...)
	cmd.Stdin = bytes.NewReader(delivery.Payload())
	cmd.Env = append(os.Environ(),
		"AXLR_BOTS_DELIVERY_ID="+delivery.ID().String(),
		"AXLR_BOTS_TASK_ID="+delivery.TaskID().String(),
		"AXLR_BOTS_BOT_ID="+delivery.BotID().String(),
		"AXLR_BOTS_EVENT="+domain.EventTaskWaitingApproval.String(),
	)
	var stderr bytes.Buffer
	cmd.Stderr = &limited{buf: &stderr, limit: 4096}
	cmd.WaitDelay = time.Second
	err := cmd.Run()
	if err == nil {
		return nil
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return fmt.Errorf("%s: timed out after %s", command[0], s.timeout())
	}
	if line := lastLine(stderr.String()); line != "" {
		return fmt.Errorf("%s: %v: %s", command[0], err, line)
	}
	return fmt.Errorf("%s: %w", command[0], err)
}

// limited keeps the first limit bytes written to it.
type limited struct {
	buf   *bytes.Buffer
	limit int
}

func (l *limited) Write(p []byte) (int, error) {
	if room := l.limit - l.buf.Len(); room > 0 {
		l.buf.Write(p[:min(len(p), room)])
	}
	return len(p), nil
}

func firstLine(text string) string {
	line, _, _ := strings.Cut(text, "\n")
	return strings.TrimSpace(line)
}

func lastLine(text string) string {
	lines := strings.Split(strings.TrimSpace(text), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}
