package unixapi

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
	"github.com/underpass-ai/axlr-bots/nodeproto"
)

// Stream opens the node's stream and answers its pushes as they come;
// the channel closes when the stream ends (ctx ended, the control plane
// went away, or it said nothing for nodeproto.StreamSilence). A control
// plane without the route is application.ErrNoStream.
func (c *Client) Stream(ctx context.Context, node domain.NodeID) (<-chan application.NodePush, error) {
	ctx, cancel := context.WithCancel(ctx)
	request, err := c.request(ctx, http.MethodGet, "/v1/nodes/"+url.PathEscape(node.String())+"/stream", nil)
	if err != nil {
		cancel()
		return nil, err
	}
	response, err := c.http.Do(request)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("control plane at %s: %w", c.address, err)
	}
	if response.StatusCode != http.StatusOK {
		defer cancel()
		defer response.Body.Close()
		raw, _ := io.ReadAll(io.LimitReader(response.Body, 1<<20))
		if response.StatusCode == http.StatusNotFound && !strings.HasPrefix(response.Header.Get("Content-Type"), "application/json") {
			// The mux's own 404: the route is not there.
			return nil, application.ErrNoStream
		}
		var failure ErrorResponse
		_ = json.Unmarshal(raw, &failure)
		return nil, fromWire(response.StatusCode, failure)
	}
	pushes := make(chan application.NodePush, 64)
	go func() {
		defer close(pushes)
		defer cancel()
		defer response.Body.Close()
		silent := time.AfterFunc(nodeproto.StreamSilence, cancel)
		defer silent.Stop()
		lines := bufio.NewScanner(response.Body)
		for lines.Scan() {
			silent.Reset(nodeproto.StreamSilence)
			var message nodeproto.StreamMessage
			if json.Unmarshal(lines.Bytes(), &message) != nil {
				continue
			}
			var push application.NodePush
			switch message.Type {
			case nodeproto.StreamResync:
				push.All = true
			case nodeproto.StreamExecution:
				execution, err := domain.NewExecutionID(message.ExecutionID)
				if err != nil {
					continue
				}
				push.Execution = execution
			default:
				continue
			}
			select {
			case pushes <- push:
			case <-ctx.Done():
				return
			}
		}
	}()
	return pushes, nil
}
