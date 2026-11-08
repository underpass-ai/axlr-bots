package notify

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/underpass-ai/axlr-bots/domain"
)

// OutputSender is the application's OutputSender: the incident outputs'
// attempts, with the approval notifications' client, timeout and
// command environment. An error names the target, never its URL: a chat
// webhook's URL is its secret, and the error goes to the audit log.
type OutputSender struct {
	// Client posts; nil is a client that follows no redirect.
	Client *http.Client
	// Timeout bounds one attempt; zero is DefaultTimeout.
	Timeout time.Duration
}

// NewOutputSender sends with the defaults.
func NewOutputSender() *OutputSender { return &OutputSender{} }

func (s *OutputSender) base() *Sender { return &Sender{Client: s.Client, Timeout: s.Timeout} }

// Send makes one attempt at the delivery.
func (s *OutputSender) Send(ctx context.Context, target domain.OutputTarget, delivery domain.OutputDelivery) error {
	ctx, cancel := context.WithTimeout(ctx, s.base().timeout())
	defer cancel()
	switch target.Kind() {
	case domain.OutputWebhook, domain.OutputSlack:
		return s.post(ctx, target, delivery)
	case domain.OutputCommand:
		return s.run(ctx, target, delivery)
	}
	return fmt.Errorf("output %s has no type a sender knows", target.Name())
}

// post sends the body; any 2xx is a delivery. A webhook's body is
// signed; a slack target's is not, its URL being the credential.
func (s *OutputSender) post(ctx context.Context, target domain.OutputTarget, delivery domain.OutputDelivery) error {
	body := delivery.Payload()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, target.URL(), bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("output %s: the request could not be made", target.Name())
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("User-Agent", "axlr-bots")
	if target.Kind() == domain.OutputWebhook {
		request.Header.Set(DeliveryHeader, delivery.ID().String())
		request.Header.Set("Idempotency-Key", delivery.ID().String())
		request.Header.Set(EventHeader, delivery.Event().String())
		request.Header.Set(SignatureHeader, target.Sign(body))
	}
	response, err := s.base().client().Do(request)
	if err != nil {
		return fmt.Errorf("output %s: %s", target.Name(), transportReason(err))
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

// transportReason says why a request did not reach the target without
// the URL that net/http puts in its errors.
func transportReason(err error) string {
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		err = urlErr.Err
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timed out"
	}
	return err.Error()
}

// run starts the program with the body on its stdin and the ids in the
// approval commands' minimal environment; exit status 0 is a delivery.
func (s *OutputSender) run(ctx context.Context, target domain.OutputTarget, delivery domain.OutputDelivery) error {
	command := target.Command()
	cmd := exec.CommandContext(ctx, command[0], command[1:]...)
	cmd.Stdin = bytes.NewReader(delivery.Payload())
	cmd.Env = append(commandEnv(os.Environ()),
		"AXLR_BOTS_DELIVERY_ID="+delivery.ID().String(),
		"AXLR_BOTS_INCIDENT_ID="+delivery.IncidentID().String(),
		"AXLR_BOTS_TENANT_ID="+delivery.TenantID().String(),
		"AXLR_BOTS_EVENT="+delivery.Event().String(),
	)
	var stderr bytes.Buffer
	cmd.Stderr = &limited{buf: &stderr, limit: 4096}
	cmd.WaitDelay = time.Second
	err := cmd.Run()
	if err == nil {
		return nil
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return fmt.Errorf("%s: timed out after %s", command[0], s.base().timeout())
	}
	if line := lastLine(stderr.String()); line != "" {
		return fmt.Errorf("%s: %v: %s", command[0], err, line)
	}
	return fmt.Errorf("%s: %w", command[0], err)
}
