package grpcapi

import (
	"google.golang.org/grpc"

	"github.com/underpass-ai/axlr-bots/adapters/unixapi"
	operatorv1 "github.com/underpass-ai/axlr-bots/api/gen/axlrbots/operator/v1"
)

// maxSendMessageBytes is the send limit of the server: above gRPC's 4 MiB
// default, for the one message the activity snapshot is (see
// activity.proto). Artifacts and transcripts go in chunks and never reach
// it.
const maxSendMessageBytes = 64 << 20

// Server serves the operator API over gRPC on the listeners it is given.
type Server struct {
	local    *unixapi.Local
	activity operatorv1.ActivityServiceServer
	unary    []grpc.UnaryServerInterceptor
	stream   []grpc.StreamServerInterceptor
}

// New serves the local service.
func New(local *unixapi.Local, options ...Option) *Server {
	server := &Server{local: local, activity: operatorv1.UnimplementedActivityServiceServer{}}
	for _, option := range options {
		option(server)
	}
	return server
}

// build is a gRPC server with the services registered. A token makes
// every call require it as a bearer; no token is the socket, which serves
// whoever reaches it, as the HTTP socket does.
func (s *Server) build(token string, options ...grpc.ServerOption) *grpc.Server {
	unary, stream := append([]grpc.UnaryServerInterceptor(nil), s.unary...), append([]grpc.StreamServerInterceptor(nil), s.stream...)
	unary = append(unary, errorInterceptor{}.unary)
	stream = append(stream, errorInterceptor{}.stream)
	if token != "" {
		auth := authInterceptor{token: token}
		unary, stream = append(unary, auth.unary), append(stream, auth.stream)
	}
	unary, stream = append(unary, actorInterceptor{}.unary), append(stream, actorInterceptor{}.stream)
	options = append(options, grpc.ChainUnaryInterceptor(unary...), grpc.ChainStreamInterceptor(stream...), grpc.MaxSendMsgSize(maxSendMessageBytes))
	server := grpc.NewServer(options...)
	s.register(server)
	return server
}

func (s *Server) register(server grpc.ServiceRegistrar) {
	operatorv1.RegisterBotsServiceServer(server, botsService{local: s.local})
	operatorv1.RegisterTasksServiceServer(server, tasksService{local: s.local})
	operatorv1.RegisterGroupsServiceServer(server, groupsService{local: s.local})
	operatorv1.RegisterExecutionsServiceServer(server, executionsService{local: s.local})
	operatorv1.RegisterNodesServiceServer(server, nodesService{local: s.local})
	operatorv1.RegisterOverviewServiceServer(server, overviewService{local: s.local})
	operatorv1.RegisterActivityServiceServer(server, s.activity)
}
