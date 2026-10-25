package nodeagent

import (
	"context"
	"encoding/json"
	"log"
	"time"

	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

// delegator carries out one execution's calls of axlr_bots_delegate: it
// asks the control plane over the node's channel and, when the call
// waits, polls the child until it ends or the parent's deadline, with
// the execution's slot lent meanwhile.
type delegator struct {
	transport application.NodeTransport
	node      domain.NodeID
	execution domain.ExecutionID
	slots     *slots
	pollWait  time.Duration
	log       *log.Logger
}

// delegationView is what the model reads about a child.
type delegationView struct {
	Task          string `json:"task"`
	Bot           string `json:"bot"`
	Status        string `json:"status"`
	Depth         int    `json:"depth"`
	Answer        string `json:"answer,omitempty"`
	FailureReason string `json:"failureReason,omitempty"`
	Note          string `json:"note,omitempty"`
}

func (d delegator) call(ctx context.Context, raw json.RawMessage) mcpToolResult {
	var args delegateArguments
	if err := json.Unmarshal(raw, &args); err != nil || args.Bot == "" || args.Objective == "" {
		return textResult("axlr_bots_delegate takes {\"bot\": \"<bot id>\", \"objective\": \"<what it should do>\", \"wait\": true|false}", true)
	}
	answer, err := d.transport.Delegate(ctx, d.node, d.execution, application.DelegateCommand{Bot: args.Bot, Objective: args.Objective, Wait: args.waits()})
	if err != nil {
		d.log.Printf("node: execution %s: delegation to %s failed: %v", d.execution, args.Bot, err)
		return textResult("the control plane did not take the delegation: "+err.Error(), true)
	}
	if answer.Refused != nil {
		d.log.Printf("node: execution %s: delegation to %s refused (%s)", d.execution, args.Bot, answer.Refused.Reason)
		return jsonResult(map[string]string{"refused": answer.Refused.Reason, "message": answer.Refused.Message}, true)
	}
	d.log.Printf("node: execution %s: delegated %s to %s at depth %d (created %v, wait %v)", d.execution, answer.TaskID, answer.BotID, answer.Depth, answer.Created, args.waits())
	if !args.waits() || answer.Done {
		return jsonResult(view(answer, ""), false)
	}
	d.slots.lend()
	defer d.slots.reclaim()
	for !answer.Done {
		left := time.Until(answer.WaitUntil)
		if left <= 0 {
			return jsonResult(view(answer, "still "+answer.Status.String()+" at this task's deadline; it was not cancelled"), false)
		}
		next, err := d.transport.AwaitDelegation(ctx, d.node, d.execution, answer.TaskID, min(left, d.pollWait))
		if ctx.Err() != nil {
			// The execution stopped (its timeout, its budget, a cancel):
			// nobody reads an answer any more.
			return textResult("the wait was cancelled", true)
		}
		if err != nil {
			d.log.Printf("node: execution %s: waiting for %s: %v", d.execution, answer.TaskID, err)
			select {
			case <-ctx.Done():
				return textResult("the wait was cancelled", true)
			case <-time.After(2 * time.Second):
			}
			continue
		}
		next.Depth = max(next.Depth, answer.Depth)
		answer = next
	}
	d.log.Printf("node: execution %s: delegated task %s ended %s", d.execution, answer.TaskID, answer.Status)
	return jsonResult(view(answer, ""), answer.Status != domain.TaskCompleted)
}

func view(answer application.DelegationAnswer, note string) delegationView {
	return delegationView{Task: answer.TaskID.String(), Bot: answer.BotID.String(), Status: answer.Status.String(), Depth: answer.Depth, Answer: answer.Answer, FailureReason: answer.FailureReason, Note: note}
}
