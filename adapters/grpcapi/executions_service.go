package grpcapi

import (
	"bytes"
	"context"
	"errors"
	"io"

	"github.com/underpass-ai/axlr-bots/adapters/unixapi"
	operatorv1 "github.com/underpass-ai/axlr-bots/api/gen/axlrbots/operator/v1"
	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

// chunkSize is the most a chunk of an artifact or a transcript carries.
const chunkSize = 64 << 10

// executionsService is ExecutionsService: executions, their artifacts and
// the transcripts the nodes upload.
type executionsService struct {
	operatorv1.UnimplementedExecutionsServiceServer
	local *unixapi.Local
}

func (s executionsService) GetExecution(ctx context.Context, request *operatorv1.GetExecutionRequest) (*operatorv1.GetExecutionResponse, error) {
	execution, err := s.local.GetExecution(ctx, request.GetExecutionId())
	if err != nil {
		return nil, err
	}
	return &operatorv1.GetExecutionResponse{Execution: executionToProto(execution)}, nil
}

func (s executionsService) GetArtifact(request *operatorv1.GetArtifactRequest, stream operatorv1.ExecutionsService_GetArtifactServer) error {
	content, err := s.local.GetArtifact(stream.Context(), request.GetExecutionId(), request.GetName())
	if err != nil {
		return err
	}
	defer content.Body.Close()
	first := &operatorv1.GetArtifactResponse{Content: &operatorv1.GetArtifactResponse_Artifact{Artifact: artifactToProto(content.Artifact)}}
	if err := stream.Send(first); err != nil {
		return err
	}
	return sendChunks(content.Body, func(chunk []byte) error {
		return stream.Send(&operatorv1.GetArtifactResponse{Content: &operatorv1.GetArtifactResponse_Chunk{Chunk: chunk}})
	})
}

func (s executionsService) ListTranscripts(ctx context.Context, request *operatorv1.ListTranscriptsRequest) (*operatorv1.ListTranscriptsResponse, error) {
	limit, err := limitOf("limit", request.GetLimit())
	if err != nil {
		return nil, err
	}
	if request.GetAfter() < 0 {
		return nil, domain.ValidationError{Field: "after", Reason: "must not be negative"}
	}
	records, err := s.local.ListTranscripts(ctx, request.GetAfter(), limit)
	if err != nil {
		return nil, err
	}
	response := &operatorv1.ListTranscriptsResponse{}
	for _, r := range records {
		response.Records = append(response.Records, transcriptRecordToProto(r))
	}
	return response, nil
}

func (s executionsService) GetTranscript(request *operatorv1.GetTranscriptRequest, stream operatorv1.ExecutionsService_GetTranscriptServer) error {
	id, err := domain.NewExecutionID(request.GetExecutionId())
	if err != nil {
		return err
	}
	record, content, err := application.NewGetTranscript(s.local.Dependencies()).Execute(stream.Context(), id, request.GetSession())
	if err != nil {
		return err
	}
	first := &operatorv1.GetTranscriptResponse{Content: &operatorv1.GetTranscriptResponse_Transcript{Transcript: transcriptRecordToProto(record)}}
	if err := stream.Send(first); err != nil {
		return err
	}
	return sendChunks(bytes.NewReader(content), func(chunk []byte) error {
		return stream.Send(&operatorv1.GetTranscriptResponse{Content: &operatorv1.GetTranscriptResponse_Chunk{Chunk: chunk}})
	})
}

// sendChunks reads the body in slices of at most chunkSize and sends each.
func sendChunks(body io.Reader, send func([]byte) error) error {
	buffer := make([]byte, chunkSize)
	for {
		n, err := body.Read(buffer)
		if n > 0 {
			if sendErr := send(append([]byte(nil), buffer[:n]...)); sendErr != nil {
				return sendErr
			}
		}
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
	}
}
