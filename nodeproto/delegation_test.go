package nodeproto

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

func TestTheDelegationOfferAndTheBudgetsTravelWithTheAssignment(t *testing.T) {
	budgets, _ := domain.NewBudgets(5, 6, 0, time.Minute)
	a := application.Assignment{ExecutionID: "exec-1", Kind: domain.ExecutionRun, Run: &application.ExecutionRequest{
		TaskID: "task-1", BotID: "bot-a", TenantID: "t", Objective: "o", Level: domain.AutonomyObserve, AllowedTools: []string{"local_read", application.DelegateToolName},
		Budgets: budgets, Timeout: time.Minute, Delegation: &application.DelegationOffer{Targets: []string{"bot-b"}, Depth: 1, MaxDepth: 2}, DelegationSocket: "/never/travels.sock",
	}}
	message := FromAssignment(a)
	data, _ := json.Marshal(message)
	var decoded AssignmentMessage
	_ = json.Unmarshal(data, &decoded)
	back, err := ToAssignment(decoded)
	if err != nil {
		t.Fatal(err)
	}
	if back.Run.Budgets.MaxModelCallsPerTask() != 5 || back.Run.Budgets.MaxToolCallsPerTask() != 6 || back.Run.Delegation == nil || back.Run.Delegation.Targets[0] != "bot-b" || back.Run.Delegation.MaxDepth != 2 || back.Run.DelegationSocket != "" {
		t.Errorf("run = %+v", back.Run)
	}
	// Without limits or delegation the fields are left out.
	a.Run.Budgets, a.Run.Delegation = domain.Budgets{}, nil
	if plain := FromAssignment(a); plain.Run.Budgets != nil || plain.Run.Delegation != nil {
		t.Errorf("plain = %+v", plain.Run)
	}
	decide := application.Assignment{ExecutionID: "exec-2", Kind: domain.ExecutionDecide, Decide: &application.DecisionRequest{
		BotID: "bot-a", SessionID: "s", Decision: "approve", Level: domain.AutonomyObserve, Delegation: &application.DelegationOffer{Targets: []string{"bot-b"}},
	}}
	backDecide, err := ToAssignment(FromAssignment(decide))
	if err != nil || backDecide.Decide.Delegation == nil {
		t.Errorf("decide = %+v %v", backDecide.Decide, err)
	}
	empty := FromAssignment(decide)
	empty.Decide.Delegation = &DelegationOfferMessage{}
	if got, _ := ToAssignment(empty); got.Decide.Delegation != nil {
		t.Error("an offer without targets was kept")
	}
	bad := FromAssignment(application.Assignment{ExecutionID: "exec-1", Kind: domain.ExecutionRun, Run: &application.ExecutionRequest{BotID: "bot-a", Objective: "o", Level: domain.AutonomyObserve}})
	bad.Run.Budgets = &BudgetsMessage{MaxModelCalls: -1}
	if _, err := ToAssignment(bad); err == nil {
		t.Error("negative budgets were taken")
	}
}

func TestDelegationAnswersRoundTrip(t *testing.T) {
	deadline := time.Date(2026, 10, 25, 20, 0, 0, 0, time.UTC)
	answer := application.DelegationAnswer{TaskID: "task-2", BotID: "bot-b", Status: domain.TaskCompleted, Created: true, Depth: 1, Grant: domain.DelegationGrant{MaxModelCalls: 3, Deadline: deadline}, Done: true, Answer: "hi", WaitUntil: deadline}
	back, err := ToDelegation(FromDelegation(answer))
	if err != nil || back.TaskID != "task-2" || back.Status != domain.TaskCompleted || !back.Created || back.Grant.MaxModelCalls != 3 || back.Answer != "hi" || !back.Done {
		t.Errorf("answer = %+v %v", back, err)
	}
	refused := application.DelegationAnswer{Refused: &domain.DelegationRefusal{Reason: domain.RefusedCycle, Message: "loop"}}
	if back, err := ToDelegation(FromDelegation(refused)); err != nil || back.Refused == nil || back.Refused.Reason != domain.RefusedCycle || back.TaskID != "" {
		t.Errorf("refused = %+v %v", back, err)
	}
	for _, bad := range []DelegationMessage{{TaskID: "Bad"}, {TaskID: "task-1", BotID: "Bad"}, {TaskID: "task-1", BotID: "bot-b", Status: "Sleeping"}} {
		if _, err := ToDelegation(bad); err == nil {
			t.Errorf("accepted %+v", bad)
		}
	}
	if back, err := ToDelegation(DelegationMessage{TaskID: "task-1", BotID: "bot-b", Status: "Pending"}); err != nil || back.Grant.MaxModelCalls != 0 {
		t.Errorf("no grant = %+v %v", back, err)
	}
}
