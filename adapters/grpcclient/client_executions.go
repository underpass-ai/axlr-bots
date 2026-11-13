package grpcclient

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"

	"github.com/underpass-ai/axlr-bots/adapters/unixapi"
	operatorv1 "github.com/underpass-ai/axlr-bots/api/gen/axlrbots/operator/v1"
	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

func (c *Client) GetExecution(ctx context.Context, id string) (domain.ExecutionSnapshot, error) {
	answer, err := c.execs.GetExecution(ctx, &operatorv1.GetExecutionRequest{ExecutionId: id})
	if err != nil {
		return domain.ExecutionSnapshot{}, fromStatus(c.address, err)
	}
	return executionSnapshot(answer.GetExecution()), nil
}

// GetArtifact opens the artifact the control plane keeps; the first
// message names it and the chunks that follow are its content, which
// Read checks against the size and SHA-256 at the end. The caller closes
// the body, which ends the stream.
func (c *Client) GetArtifact(ctx context.Context, execution, name string) (unixapi.ArtifactContent, error) {
	ctx, cancel := context.WithCancel(ctx)
	stream, err := c.execs.GetArtifact(ctx, &operatorv1.GetArtifactRequest{ExecutionId: execution, Name: name})
	if err != nil {
		cancel()
		return unixapi.ArtifactContent{}, fromStatus(c.address, err)
	}
	first, err := stream.Recv()
	if err != nil {
		cancel()
		return unixapi.ArtifactContent{}, fromStatus(c.address, err)
	}
	if first.GetArtifact() == nil {
		cancel()
		return unixapi.ArtifactContent{}, fmt.Errorf("control plane at %s: artifact %s: the stream did not start with the artifact", c.address, name)
	}
	artifact := artifactOf(first.GetArtifact())
	artifact.Stored = true
	return unixapi.ArtifactContent{Artifact: artifact, Body: newArtifactBody(artifactChunks{stream: stream}, cancel, artifact, c.address)}, nil
}

func (c *Client) ListTranscripts(ctx context.Context, after int64, limit int) ([]application.TranscriptRecord, error) {
	answer, err := c.execs.ListTranscripts(ctx, &operatorv1.ListTranscriptsRequest{After: after, Limit: int32(limit)})
	if err != nil {
		return nil, fromStatus(c.address, err)
	}
	records := make([]application.TranscriptRecord, len(answer.GetRecords()))
	for i, record := range answer.GetRecords() {
		records[i] = transcriptRecordOf(record)
	}
	return records, nil
}

// GetTranscript reads one transcript whole, in chunks, and checks it
// against the size and SHA-256 of the record the first message carries.
func (c *Client) GetTranscript(ctx context.Context, execution, session string) ([]byte, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	stream, err := c.execs.GetTranscript(ctx, &operatorv1.GetTranscriptRequest{ExecutionId: execution, Session: session})
	if err != nil {
		return nil, fromStatus(c.address, err)
	}
	var content bytes.Buffer
	var record *operatorv1.TranscriptRecord
	for {
		message, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fromStatus(c.address, err)
		}
		if message.GetTranscript() != nil {
			record = message.GetTranscript()
			continue
		}
		if content.Len()+len(message.GetChunk()) > application.MaxTranscriptBytes {
			return nil, fmt.Errorf("transcript %s/%s is larger than %d bytes", execution, session, application.MaxTranscriptBytes)
		}
		content.Write(message.GetChunk())
	}
	if record == nil {
		return nil, fmt.Errorf("control plane at %s: transcript %s/%s: the stream did not start with its record", c.address, execution, session)
	}
	sum := sha256.Sum256(content.Bytes())
	if hex.EncodeToString(sum[:]) != record.GetSha256() || int64(content.Len()) != record.GetSize() {
		return nil, fmt.Errorf("transcript %s/%s does not match the size and SHA-256 the control plane answered", execution, session)
	}
	return content.Bytes(), nil
}
