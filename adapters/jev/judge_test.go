package jev

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestClientChecksTheKeyAndTheModel(t *testing.T) {
	for _, c := range []ClientConfig{{}, {APIKey: "k\nx"}, {APIKey: "k", Model: "jev-latest"}, {APIKey: "k", Model: "bad model"}} {
		if _, err := NewClient(c); err == nil {
			t.Errorf("%+v accepted", c)
		}
	}
	client, err := NewClient(ClientConfig{APIKey: " k "})
	if err != nil || client.Model() != DefaultModel {
		t.Fatalf("client = %+v %v", client, err)
	}
}

func TestQuestionValidation(t *testing.T) {
	good := Question{State: "s", Question: "q", Options: []string{"a", "b"}}
	if err := good.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []Question{
		{Question: "q", Options: []string{"a", "b"}},
		{State: "s", Options: []string{"a", "b"}},
		{State: "s", Question: "q", Options: []string{"a"}},
		{State: "s", Question: "q", Options: []string{"a", "a"}},
		{State: "s", Question: "q", Options: []string{"a", ""}},
		{State: strings.Repeat("x", MaxStateBytes+1), Question: "q", Options: []string{"a", "b"}},
	} {
		if err := bad.Validate(); err == nil {
			t.Errorf("%+v accepted", bad)
		}
	}
}

// The answer is checked as AXLR checks it: the model asked, one answer
// to the one question, a choice among the options with a probability
// for each, and a confidence.
func TestTheVerdictIsCheckedAgainstTheQuestion(t *testing.T) {
	var body string
	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer k" || r.Header.Get("Content-Type") != "application/json" {
			rw.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = rw.Write([]byte(body))
	}))
	defer server.Close()
	client, _ := NewClient(ClientConfig{APIKey: "k", Endpoint: server.URL, Timeout: time.Second})
	question := Question{State: "s", Question: "q", Options: []string{"a", "b"}}
	body = `{"model": "jev-1.13.0", "usage": {"input_tokens": 42}, "answers": {"q": {"type": "choice", "choice": "b", "probabilities": {"a": 0.3, "b": 0.7}, "confidence": 0.9}}}`
	verdict, err := client.Choose(context.Background(), question)
	if err != nil || verdict.Choice != "b" || verdict.Probability != 0.7 || verdict.Confidence != 0.9 || verdict.InputTokens != 42 || verdict.Model != DefaultModel {
		t.Fatalf("verdict = %+v %v", verdict, err)
	}
	for name, bad := range map[string]string{
		"another model":   `{"model": "jev-9", "answers": {"q": {"type": "choice", "choice": "b", "probabilities": {"a": 0.3, "b": 0.7}, "confidence": 0.9}}}`,
		"not offered":     `{"model": "jev-1.13.0", "answers": {"q": {"type": "choice", "choice": "c", "probabilities": {"a": 0.3, "b": 0.7}, "confidence": 0.9}}}`,
		"null prob":       `{"model": "jev-1.13.0", "answers": {"q": {"type": "choice", "choice": "b", "probabilities": {"a": null, "b": 0.7}, "confidence": 0.9}}}`,
		"no confidence":   `{"model": "jev-1.13.0", "answers": {"q": {"type": "choice", "choice": "b", "probabilities": {"a": 0.3, "b": 0.7}}}}`,
		"two answers":     `{"model": "jev-1.13.0", "answers": {"q": {"type": "choice", "choice": "b", "probabilities": {"a": 0.3, "b": 0.7}, "confidence": 0.9}, "r": {}}}`,
		"not json":        `nope`,
		"a yes/no answer": `{"model": "jev-1.13.0", "answers": {"q": {"type": "noul", "noul": 0.5}}}`,
	} {
		body = bad
		if _, err := client.Choose(context.Background(), question); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if _, err := client.Choose(context.Background(), Question{}); err == nil {
		t.Error("an empty question was sent")
	}
}

func TestTheClientRetriesOn429AndNamesOtherFailures(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
		calls++
		switch calls {
		case 1:
			rw.Header().Set("Retry-After", "0")
			rw.WriteHeader(http.StatusTooManyRequests)
		default:
			_, _ = rw.Write([]byte(`{"model": "jev-1.13.0", "answers": {"q": {"type": "choice", "choice": "a", "probabilities": {"a": 0.6, "b": 0.4}, "confidence": 0.5}}}`))
		}
	}))
	defer server.Close()
	client, _ := NewClient(ClientConfig{APIKey: "k", Endpoint: server.URL, Timeout: time.Second})
	question := Question{State: "s", Question: "q", Options: []string{"a", "b"}}
	if verdict, err := client.Choose(context.Background(), question); err != nil || verdict.Choice != "a" || calls != 2 {
		t.Fatalf("verdict = %+v %v after %d calls", verdict, err, calls)
	}
	for code, want := range map[int]string{http.StatusForbidden: "refused the API key", http.StatusInternalServerError: "HTTP 500"} {
		failing := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) { rw.WriteHeader(code) }))
		client, _ := NewClient(ClientConfig{APIKey: "k", Endpoint: failing.URL, Timeout: time.Second})
		if _, err := client.Choose(context.Background(), question); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("HTTP %d: %v", code, err)
		}
		failing.Close()
	}
	down, _ := NewClient(ClientConfig{APIKey: "k", Endpoint: "http://127.0.0.1:1", Timeout: time.Second})
	if _, err := down.Choose(context.Background(), question); err == nil || !strings.Contains(err.Error(), "did not answer") {
		t.Errorf("down: %v", err)
	}
}
