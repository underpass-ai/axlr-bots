package grpcclient

import operatorv1 "github.com/underpass-ai/axlr-bots/api/gen/axlrbots/operator/v1"

// artifactChunks adapts the artifact stream to chunkStream.
type artifactChunks struct {
	stream operatorv1.ExecutionsService_GetArtifactClient
}

func (s artifactChunks) next() ([]byte, error) {
	message, err := s.stream.Recv()
	if err != nil {
		return nil, err
	}
	return message.GetChunk(), nil
}
