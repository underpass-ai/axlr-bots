package unixapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

// UploadTranscript puts a session's whole transcript to the control
// plane, before the report, with its SHA-256 in the header.
func (c *Client) UploadTranscript(ctx context.Context, node domain.NodeID, execution domain.ExecutionID, upload application.TranscriptUpload) error {
	path := "/v1/nodes/" + url.PathEscape(node.String()) + "/executions/" + url.PathEscape(execution.String()) + "/transcripts/" + url.PathEscape(upload.Session)
	request, err := c.request(ctx, http.MethodPut, path, bytes.NewReader(upload.Content))
	if err != nil {
		return err
	}
	request.ContentLength = int64(len(upload.Content))
	if len(upload.Content) == 0 {
		request.Body = http.NoBody
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set(TranscriptSHA256Header, upload.SHA256)
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
