package unixapi

import (
	"encoding/json"
	"net/http"

	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
	"github.com/underpass-ai/axlr-bots/nodeproto"
)

// maxProxyBody bounds what a node may put in one proxied call.
const maxProxyBody = 256 << 10

func (s *Server) proxyModel(w http.ResponseWriter, r *http.Request) {
	node, execution, ok := s.proxyTarget(w, r)
	if !ok {
		return
	}
	var request nodeproto.ModelRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxProxyBody)).Decode(&request); err != nil {
		respond(w, 0, nil, domain.ValidationError{Field: "body", Reason: "must be a model request"})
		return
	}
	messages := make([]application.ModelChat, 0, len(request.Messages))
	for _, message := range request.Messages {
		messages = append(messages, application.ModelChat{Role: message.Role, Content: message.Content})
	}
	tools := make([]application.ModelToolSpec, 0, len(request.Tools))
	for _, tool := range request.Tools {
		tools = append(tools, application.ModelToolSpec{Name: tool.Name, Description: tool.Description, Parameters: tool.Parameters})
	}
	answer, err := s.models.Complete(r.Context(), node, execution, messages, tools)
	if err != nil {
		respond(w, 0, nil, err)
		return
	}
	reply := nodeproto.ModelReply{Text: answer.Text, PromptTokens: answer.PromptTokens, CompletionTokens: answer.CompletionTokens}
	for _, call := range answer.ToolCalls {
		reply.ToolCalls = append(reply.ToolCalls, nodeproto.ModelToolCallMessage{CallID: call.CallID, Name: call.Name, Arguments: call.Arguments})
	}
	respond(w, http.StatusOK, reply, nil)
}

func (s *Server) proxyJev(w http.ResponseWriter, r *http.Request) {
	node, execution, ok := s.proxyTarget(w, r)
	if !ok {
		return
	}
	var request nodeproto.JevRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxProxyBody)).Decode(&request); err != nil {
		respond(w, 0, nil, domain.ValidationError{Field: "body", Reason: "must be a Jev request"})
		return
	}
	verdict, err := s.models.Choose(r.Context(), node, execution, application.JudgeQuestion{State: request.State, Question: request.Question, Options: request.Options})
	if err != nil {
		respond(w, 0, nil, err)
		return
	}
	respond(w, http.StatusOK, nodeproto.JevReply{Choice: verdict.Choice, Probability: verdict.Probability, Confidence: verdict.Confidence}, nil)
}

func (s *Server) proxyTarget(w http.ResponseWriter, r *http.Request) (domain.NodeID, domain.ExecutionID, bool) {
	node, err := domain.NewNodeID(r.PathValue("id"))
	if err != nil {
		respond(w, 0, nil, err)
		return "", "", false
	}
	execution, err := domain.NewExecutionID(r.PathValue("exec"))
	if err != nil {
		respond(w, 0, nil, err)
		return "", "", false
	}
	return node, execution, true
}
