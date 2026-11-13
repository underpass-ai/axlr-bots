package unixapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

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

// ListTranscripts reads the records of the transcripts kept after the
// cursor, at most limit (0: the control plane's bound).
func (c *Client) ListTranscripts(ctx context.Context, after int64, limit int) ([]application.TranscriptRecord, error) {
	query := url.Values{}
	query.Set("after", strconv.FormatInt(after, 10))
	if limit > 0 {
		query.Set("limit", strconv.Itoa(limit))
	}
	records := []application.TranscriptRecord{}
	return records, c.call(ctx, http.MethodGet, "/v1/transcripts?"+query.Encode(), nil, &records)
}

// GetTranscript reads one transcript whole and checks it against the
// SHA-256 the control plane answers.
func (c *Client) GetTranscript(ctx context.Context, execution, session string) ([]byte, error) {
	request, err := c.request(ctx, http.MethodGet, "/v1/executions/"+url.PathEscape(execution)+"/transcripts/"+url.PathEscape(session), nil)
	if err != nil {
		return nil, err
	}
	response, err := c.http.Do(request)
	if err != nil {
		return nil, fmt.Errorf("control plane at %s: %w", c.address, err)
	}
	defer response.Body.Close()
	if response.StatusCode >= 400 {
		raw, _ := io.ReadAll(io.LimitReader(response.Body, 1<<20))
		var failure ErrorResponse
		_ = json.Unmarshal(raw, &failure)
		return nil, fromWire(response.StatusCode, failure)
	}
	content, err := io.ReadAll(io.LimitReader(response.Body, application.MaxTranscriptBytes+1))
	if err != nil {
		return nil, fmt.Errorf("control plane at %s: %w", c.address, err)
	}
	sum := sha256.Sum256(content)
	if !strings.EqualFold(hex.EncodeToString(sum[:]), response.Header.Get(TranscriptSHA256Header)) {
		return nil, fmt.Errorf("transcript %s/%s does not match the SHA-256 the control plane answered", execution, session)
	}
	return content, nil
}
