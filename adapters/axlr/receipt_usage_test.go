package axlr

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/underpass-ai/axlr-bots/application"
)

// launch is when the fake run's process started.
var launch = time.Date(2026, 11, 5, 0, 3, 16, 0, time.UTC)

// receiptLine is a receipt with the given request rows.
func receiptLine(rows string) []byte {
	return []byte(`{"type":"receipt","receipt":{"schema":"axlr.execution-receipt/v1","status":"completed","complete":true,"requests":[` + rows + `]}}`)
}

// cumulativeResult reports the whole session's usage, as AXLR does.
var cumulativeResult = []byte(`{"type":"result","result":{"schema":"axlr.execution-result/v1","status":"completed","session_id":"sess-1","sessions":["sess-1"],"answer":"ok","tool_calls":[{"call_id":"c1","tool":"local_read","status":"completed"}],"usage":{"requests":3,"prompt_tokens":300,"cached_tokens":100,"completion_tokens":30,"reasoning_tokens":3,"cost_usd":0.006,"cost_requests":3}}}`)

const (
	earlierRow  = `{"session_id":"sess-1","index":1,"started_at":"2026-11-05T00:00:03Z","prompt_tokens":100,"cached_tokens":0,"completion_tokens":10,"reasoning_tokens":1,"cost_usd":0.002}`
	earlierRow2 = `{"session_id":"sess-1","index":2,"started_at":"2026-11-05T00:01:00Z","prompt_tokens":100,"cached_tokens":50,"completion_tokens":10,"reasoning_tokens":1,"cost_usd":0.002}`
	ownRow      = `{"session_id":"sess-1","index":3,"started_at":"2026-11-05T00:03:20Z","prompt_tokens":100,"cached_tokens":50,"completion_tokens":10,"reasoning_tokens":1,"cost_usd":0.002}`
)

func near(a, b float64) bool { return math.Abs(a-b) < 1e-12 }

func parse(since time.Time, lines ...[]byte) application.ExecutionReport {
	p := newTurnParser(since)
	for _, line := range lines {
		p.feed(line)
	}
	return p.report(exitCompleted, "")
}

func TestAContinuedSessionCountsOnlyItsOwnRequests(t *testing.T) {
	report := parse(launch, receiptLine(earlierRow+","+earlierRow2+","+ownRow), cumulativeResult)
	u := report.Usage
	if u.Requests != 1 || u.PromptTokens != 100 || u.CachedTokens != 50 || u.CompletionTokens != 10 || u.ReasoningTokens != 1 || !near(u.CostUSD, 0.002) {
		t.Errorf("usage = %+v, want only the request after the launch", u)
	}
	if u.ToolCalls != 1 {
		t.Errorf("tool calls = %d", u.ToolCalls)
	}
}

func TestAFreshRunCountsEveryRequest(t *testing.T) {
	report := parse(time.Date(2026, 11, 5, 0, 0, 0, 0, time.UTC), receiptLine(earlierRow+","+earlierRow2+","+ownRow), cumulativeResult)
	if u := report.Usage; u.Requests != 3 || !near(u.CostUSD, 0.006) || u.PromptTokens != 300 {
		t.Errorf("usage = %+v, want all three requests summed", u)
	}
}

func TestSuccessorSessionsOfTheRunAreSummed(t *testing.T) {
	successor := `{"session_id":"sess-2","index":1,"started_at":"2026-11-05T00:04:00Z","prompt_tokens":40,"cached_tokens":0,"completion_tokens":4,"reasoning_tokens":0,"cost_usd":0.0005}`
	report := parse(launch, receiptLine(earlierRow+","+ownRow+","+successor), cumulativeResult)
	if u := report.Usage; u.Requests != 2 || !near(u.CostUSD, 0.0025) || u.PromptTokens != 140 {
		t.Errorf("usage = %+v, want the run's request and its successor's", u)
	}
}

func TestARequestWithoutCostCountsButCostsNothing(t *testing.T) {
	failed := `{"session_id":"sess-1","index":4,"started_at":"2026-11-05T00:03:30Z","status":"failed","prompt_tokens":0,"completion_tokens":0,"cost_usd":null}`
	report := parse(launch, receiptLine(ownRow+","+failed), cumulativeResult)
	if u := report.Usage; u.Requests != 2 || !near(u.CostUSD, 0.002) {
		t.Errorf("usage = %+v, want two requests and one cost", u)
	}
}

func TestAnInterruptedEarlierRunStaysOutOfTheCost(t *testing.T) {
	interrupted := `{"session_id":"sess-1","index":2,"started_at":"2026-11-05T00:02:00Z","status":"interrupted","unmetered":true,"cost_usd":null}`
	report := parse(launch, receiptLine(earlierRow+","+interrupted+","+ownRow), cumulativeResult)
	if u := report.Usage; u.Requests != 1 || !near(u.CostUSD, 0.002) {
		t.Errorf("usage = %+v", u)
	}
}

func TestAReceiptWithoutRequestsKeepsTheReportedUsage(t *testing.T) {
	bare := []byte(`{"type":"receipt","receipt":{"schema":"axlr.execution-receipt/v1","status":"completed","complete":true}}`)
	report := parse(launch, bare, cumulativeResult)
	if u := report.Usage; u.Requests != 3 || !near(u.CostUSD, 0.006) {
		t.Errorf("usage = %+v, want the result's usage", u)
	}
	if report := parse(launch, cumulativeResult); report.Usage.Requests != 3 {
		t.Errorf("without a receipt usage = %+v", report.Usage)
	}
}

func TestUsageSinceCannotTell(t *testing.T) {
	for name, c := range map[string]struct {
		raw   string
		since time.Time
	}{
		"no launch time": {`{"requests":[]}`, time.Time{}},
		"not an object":  {`"no"`, launch},
		"no requests":    {`{"requests":null}`, launch},
	} {
		if _, ok := usageSince([]byte(c.raw), c.since); ok {
			t.Errorf("%s: usage was told", name)
		}
	}
	if u, ok := usageSince([]byte(`{"requests":[]}`), launch); !ok || u.Requests != 0 || u.CostUSD != 0 {
		t.Errorf("an empty list = %+v %v", u, ok)
	}
}

func TestANegativeCostIsNotSubtracted(t *testing.T) {
	refund := `{"session_id":"sess-1","index":3,"started_at":"2026-11-05T00:03:20Z","cost_usd":-0.5}`
	if u, ok := usageSince([]byte(`{"requests":[`+refund+`]}`), launch); !ok || u.CostUSD != 0 || u.Requests != 1 {
		t.Errorf("usage = %+v %v", u, ok)
	}
}

func TestADecisionReportsTheCostOfItsOwnRun(t *testing.T) {
	e, _ := fake(t, "decide-continues")
	report, err := e.Decide(context.Background(), application.DecisionRequest{ExecutionID: "exec-2", BotID: "sii", SessionID: "sess-1", CallID: "c2", Decision: "approve", Timeout: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	if u := report.Usage; u.Requests != 1 || u.PromptTokens != 7800 || u.CachedTokens != 7600 || !near(u.CostUSD, 0.00011) {
		t.Errorf("usage = %+v, want only this run's request (the session's total is $0.0018)", u)
	}
}
