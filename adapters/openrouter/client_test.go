package openrouter

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/underpass-ai/axlr-bots/application"
)

func answering(t *testing.T, status int, body string, seen func(*http.Request, map[string]any)) *Client {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		var decoded map[string]any
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Errorf("request body: %v", err)
		}
		if seen != nil {
			seen(r, decoded)
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	client, err := NewClient("key-1", server.URL)
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func TestNewClientNeedsAKey(t *testing.T) {
	for _, key := range []string{"", "  ", "a\nb"} {
		if _, err := NewClient(key, ""); err == nil {
			t.Errorf("key %q accepted", key)
		}
	}
	client, err := NewClient(" key ", "")
	if err != nil || client.endpoint != Endpoint || client.key != "key" {
		t.Fatalf("default endpoint: %v %+v", err, client)
	}
}

func TestCompleteCarriesTheCallAndReadsTheToolCalls(t *testing.T) {
	body := `{"choices":[{"message":{"content":"done","tool_calls":[{"id":"call-1","function":{"name":"gpio_write","arguments":"{\"pin\":\"2\"}"}}]}}],"usage":{"prompt_tokens":12,"completion_tokens":4}}`
	client := answering(t, http.StatusOK, body, func(r *http.Request, got map[string]any) {
		if r.Header.Get("Authorization") != "Bearer key-1" {
			t.Errorf("authorization = %q", r.Header.Get("Authorization"))
		}
		if got["model"] != "m-1" || got["max_tokens"] != float64(maxOutputTokens) {
			t.Errorf("request = %+v", got)
		}
		tools, _ := got["tools"].([]any)
		if len(tools) != 1 {
			t.Errorf("tools = %+v", got["tools"])
		}
	})
	answer, err := client.Complete(context.Background(), application.ModelCall{
		Model:    "m-1",
		Messages: []application.ModelChat{{Role: "user", Content: "light"}},
		Tools:    []application.ModelToolSpec{{Name: "gpio_write", Description: "Set a pin", Parameters: json.RawMessage(`{"type":"object"}`)}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if answer.Text != "done" || answer.PromptTokens != 12 || answer.CompletionTokens != 4 {
		t.Errorf("answer = %+v", answer)
	}
	if len(answer.ToolCalls) != 1 || answer.ToolCalls[0].CallID != "call-1" || answer.ToolCalls[0].Name != "gpio_write" || string(answer.ToolCalls[0].Arguments) != `{"pin":"2"}` {
		t.Errorf("tool calls = %+v", answer.ToolCalls)
	}
}

func TestCompleteLeavesOutAnEmptyToolList(t *testing.T) {
	client := answering(t, http.StatusOK, `{"choices":[{"message":{"content":"hi"}}]}`, func(_ *http.Request, got map[string]any) {
		if _, ok := got["tools"]; ok {
			t.Errorf("tools sent: %+v", got["tools"])
		}
	})
	if answer, err := client.Complete(context.Background(), application.ModelCall{Model: "m"}); err != nil || answer.Text != "hi" {
		t.Errorf("complete: %v %+v", err, answer)
	}
}

func TestCompleteReportsAFailureWithoutTheKey(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		want   string
	}{
		{"refused", http.StatusUnauthorized, `{"error":"bad key-1"}`, "HTTP 401"},
		{"not JSON", http.StatusOK, `<html>`, "not JSON"},
		{"no choice", http.StatusOK, `{"choices":[]}`, "no choice"},
		{"bad arguments", http.StatusOK, `{"choices":[{"message":{"tool_calls":[{"id":"c","function":{"name":"x","arguments":"{"}}]}}]}`, "not JSON"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := answering(t, c.status, c.body, nil).Complete(context.Background(), application.ModelCall{Model: "m"})
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err = %v, want %q", err, c.want)
			}
			if strings.Contains(err.Error(), "key-1") {
				t.Errorf("the key leaked: %v", err)
			}
		})
	}
}

func TestCompleteReportsAnUnreachableProvider(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	url := server.URL
	server.Close()
	client, err := NewClient("key-1", url)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Complete(context.Background(), application.ModelCall{Model: "m"}); err == nil || !strings.Contains(err.Error(), "did not answer") {
		t.Errorf("err = %v", err)
	}
}
