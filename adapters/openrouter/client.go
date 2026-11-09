// Package openrouter reaches a language model through OpenRouter's chat
// completions API, for the calls the control plane makes on behalf of a
// node that carries no key.
package openrouter

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/underpass-ai/axlr-bots/application"
)

// Endpoint is OpenRouter's chat completions API.
const Endpoint = "https://openrouter.ai/api/v1/chat/completions"

const (
	maxOutputTokens = 1024
	maxBodyBytes    = 1 << 20
	callTimeout     = 60 * time.Second
)

// Client is OpenRouter with one key. The key goes in the Authorization
// header and nowhere else.
type Client struct {
	key      string
	endpoint string
	http     http.Client
}

// NewClient checks the key; an empty endpoint is OpenRouter's.
func NewClient(key, endpoint string) (*Client, error) {
	key = strings.TrimSpace(key)
	if key == "" || strings.ContainsAny(key, "\r\n") {
		return nil, errors.New("OPENROUTER_API_KEY is required")
	}
	if endpoint == "" {
		endpoint = Endpoint
	}
	return &Client{key: key, endpoint: endpoint, http: http.Client{Timeout: callTimeout}}, nil
}

// Complete asks the model once.
func (c *Client) Complete(ctx context.Context, call application.ModelCall) (application.ModelAnswer, error) {
	body, err := json.Marshal(wireRequest(call))
	if err != nil {
		return application.ModelAnswer{}, errors.New("the model request could not be written")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return application.ModelAnswer{}, errors.New("could not create the model request")
	}
	request.Header.Set("Authorization", "Bearer "+c.key)
	request.Header.Set("Content-Type", "application/json")
	response, err := c.http.Do(request)
	if err != nil {
		return application.ModelAnswer{}, errors.New("OpenRouter did not answer")
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, maxBodyBytes))
	if err != nil {
		return application.ModelAnswer{}, errors.New("OpenRouter did not answer")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return application.ModelAnswer{}, fmt.Errorf("OpenRouter answered HTTP %d", response.StatusCode)
	}
	return parseAnswer(data)
}

func wireRequest(call application.ModelCall) map[string]any {
	messages := make([]map[string]string, 0, len(call.Messages))
	for _, message := range call.Messages {
		messages = append(messages, map[string]string{"role": message.Role, "content": message.Content})
	}
	request := map[string]any{"model": call.Model, "max_tokens": maxOutputTokens, "messages": messages}
	if len(call.Tools) > 0 {
		tools := make([]map[string]any, 0, len(call.Tools))
		for _, tool := range call.Tools {
			tools = append(tools, map[string]any{"type": "function", "function": map[string]any{
				"name": tool.Name, "description": tool.Description, "parameters": tool.Parameters,
			}})
		}
		request["tools"] = tools
	}
	return request
}

func parseAnswer(data []byte) (application.ModelAnswer, error) {
	var wire struct {
		Choices []struct {
			Message struct {
				Content   string `json:"content"`
				ToolCalls []struct {
					ID       string `json:"id"`
					Function struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"message"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return application.ModelAnswer{}, errors.New("the answer from OpenRouter is not JSON")
	}
	if len(wire.Choices) == 0 {
		return application.ModelAnswer{}, errors.New("OpenRouter answered no choice")
	}
	message := wire.Choices[0].Message
	answer := application.ModelAnswer{Text: message.Content, PromptTokens: wire.Usage.PromptTokens, CompletionTokens: wire.Usage.CompletionTokens}
	for _, call := range message.ToolCalls {
		arguments := json.RawMessage(call.Function.Arguments)
		if !json.Valid(arguments) {
			return application.ModelAnswer{}, errors.New("OpenRouter sent tool arguments that are not JSON")
		}
		answer.ToolCalls = append(answer.ToolCalls, application.ModelToolCall{CallID: call.ID, Name: call.Function.Name, Arguments: arguments})
	}
	return answer, nil
}
