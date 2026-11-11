package application

import (
	"context"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/underpass-ai/axlr-bots/domain"
)

// channelNode registers an Online node with the team channel on that
// shares the memory of the teams it names.
func channelNode(t *testing.T, h *harness, id string, memoryTeams ...string) {
	t.Helper()
	switchedNode(t, h, id, true, memoryTeams...)
}

// switchedNode registers an Online node with the team channel on or off,
// sharing the memory of the teams it names; off and none is an older
// node. Registering it again switches it.
func switchedNode(t *testing.T, h *harness, id string, on bool, memoryTeams ...string) {
	t.Helper()
	r := registration(id)
	r.Capabilities.MemoryTeams = memoryTeams
	r.Capabilities.TeamChannel = on
	if _, err := NewRegisterNode(h.deps).Execute(context.Background(), r); err != nil {
		t.Fatal(err)
	}
}

// channelBot registers and enables a bot of the team; ask names the teams
// it asks, from the teams whose questions it answers.
func channelBot(t *testing.T, h *harness, id string, groups []string, ask *domain.AskSnapshot, from []string, nodes ...string) domain.Bot {
	t.Helper()
	ctx := context.Background()
	d := sampleDefinition()
	d.ID, d.Name, d.Tenant = id, id, "default"
	d.AllowedTools = []string{"local_read"}
	d.Groups, d.Ask = groups, ask
	d.Triggers = []domain.TriggerSnapshot{{Type: "manual"}}
	if len(from) > 0 {
		d.Triggers = append(d.Triggers, domain.TriggerSnapshot{Type: "question", From: from})
	}
	d.Placement = domain.PlacementSnapshot{Nodes: nodes}
	bot, err := NewRegisterBot(h.deps).Execute(ctx, d)
	if err != nil {
		t.Fatal(err)
	}
	if bot, err = NewEnableBot(h.deps).Execute(ctx, bot.ID()); err != nil {
		t.Fatal(err)
	}
	return bot
}

// channel is a QA bot that asks dev, and two dev bots that answer QA, and
// a dev reviewer that answers nobody; dev-node shares dev's memory.
func channel(t *testing.T) (*harness, Assignment) {
	t.Helper()
	h := newHarness()
	channelNode(t, h, "qa-node")
	channelNode(t, h, "dev-node", "dev")
	asker := channelBot(t, h, "qa-jefe", []string{"qa"}, &domain.AskSnapshot{Teams: []string{"dev"}, PerMinute: 10, MaxOpen: 8}, nil, "qa-node")
	channelBot(t, h, "dev-b", []string{"dev"}, nil, []string{"qa"}, "dev-node")
	channelBot(t, h, "dev-a", []string{"dev"}, nil, []string{"qa", "producto"}, "dev-node")
	channelBot(t, h, "dev-revisor", []string{"dev"}, nil, nil, "dev-node")
	return h, runOnNode(t, h, asker, "qa-node", "lead-1")
}

func askTeam(t *testing.T, h *harness, node domain.NodeID, execution domain.ExecutionID, team, question string) QuestionAnswer {
	t.Helper()
	answer, err := NewAskTeam(h.deps).Execute(context.Background(), node, execution, AskTeamCommand{Team: team, Question: question})
	if err != nil {
		t.Fatal(err)
	}
	return answer
}

func TestAQuestionBecomesOneTaskOfOneConsentingMember(t *testing.T) {
	h, parent := channel(t)
	if parent.Run.Ask == nil || len(parent.Run.Ask.Teams) != 1 || parent.Run.Ask.Teams[0] != "dev" {
		t.Fatalf("the asker is offered no ask tool: %+v", parent.Run.Ask)
	}
	if !slices.Contains(parent.Run.NodeTools(), AskToolName) || !slices.Contains(parent.Run.AllowedTools, AskToolName) || !slices.Contains(parent.Run.NodeTools(), QuestionStatusToolName) || !slices.Contains(parent.Run.AllowedTools, QuestionStatusToolName) {
		t.Errorf("node tools %v allowed %v", parent.Run.NodeTools(), parent.Run.AllowedTools)
	}
	answer := askTeam(t, h, "qa-node", parent.ExecutionID, "dev", "Which release fixed the importer?")
	if answer.Refused != nil || !answer.Created || answer.BotID != "dev-a" || answer.Status != domain.TaskPending {
		t.Fatalf("answer = %+v %+v", answer, answer.Refused)
	}
	child, err := h.tasks.Get(context.Background(), answer.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	if child.Trigger() != (domain.TriggerRef{Type: domain.TriggerQuestion, Name: "qa"}) || child.IsUrgent() {
		t.Errorf("trigger = %+v urgent %v", child.Trigger(), child.IsUrgent())
	}
	objective := child.Objective().String()
	if !strings.HasPrefix(objective, "Team qa asks you through the team channel, for its bot qa-jefe.") || !strings.Contains(objective, "\nWhich release fixed the importer?\nQUESTION-") || !strings.Contains(objective, "project:dev") {
		t.Errorf("objective:\n%s", objective)
	}
	parentTask, _ := h.tasks.Get(context.Background(), parent.Run.TaskID)
	if child.CorrelationID() != parentTask.CorrelationID() {
		t.Errorf("trace %s, the asker's %s", child.CorrelationID(), parentTask.CorrelationID())
	}
	lineage, ok := child.Lineage()
	if !ok || lineage.ParentTaskID != parentTask.ID().String() || lineage.Depth != 1 || lineage.MaxDepth != 1 || lineage.Chain[0] != "qa-jefe" {
		t.Errorf("lineage = %+v", lineage)
	}
	// Its own budgets, and two hours from the ask.
	if lineage.Grant.MaxModelCalls != 0 || lineage.Grant.MaxToolCalls != 0 || !lineage.Grant.Deadline.Equal(t0.Add(2*time.Hour)) {
		t.Errorf("grant = %+v", lineage.Grant)
	}
	if !strings.Contains(string(child.Input()), `"fromTeam":"qa"`) || strings.Contains(string(child.Input()), "wait") || strings.Contains(string(child.Input()), "nonce") {
		t.Errorf("input = %s", child.Input())
	}
	// The question takes nothing of the asker's budgets.
	if q := parentTask.Questions(); len(q) != 1 || q[0] != child.ID() || len(parentTask.Children()) != 0 {
		t.Errorf("questions %v children %v", q, parentTask.Children())
	}
	if model, tools := parentTask.Granted(); model != 0 || tools != 0 {
		t.Errorf("granted %d %d", model, tools)
	}
	asked := h.log.last(domain.EventQuestionAsked)
	if asked.BotID != "qa-jefe" || asked.Payload["responder"] != "dev-a" || asked.Payload["deduplicated"] != false || asked.Payload["team"] != "dev" {
		t.Errorf("asked = %+v", asked)
	}
	if audit := h.log.audits[len(h.log.audits)-1]; audit.Action != "question.ask" || audit.Outcome != domain.AuditOK {
		t.Errorf("audit = %+v", audit)
	}
}

// Two questions asked at once to one team land on two bots: the pick runs
// under the runner's lock, so the second sees the first's task.
func TestTwoQuestionsAtOnceLandOnTwoBots(t *testing.T) {
	h, parent := channel(t)
	runner := NewSerialRunner(h.deps)
	var wg sync.WaitGroup
	answers := make([]QuestionAnswer, 2)
	errs := make([]error, 2)
	for i, question := range []string{"Who owns the importer?", "Is v0.16 on staging?"} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			answers[i], errs[i] = runner.Ask(context.Background(), "qa-node", parent.ExecutionID, AskTeamCommand{Team: "dev", Question: question})
		}()
	}
	wg.Wait()
	for i := range answers {
		if errs[i] != nil || answers[i].Refused != nil || !answers[i].Created {
			t.Fatalf("answer %d = %+v %v", i, answers[i], errs[i])
		}
	}
	got := []string{answers[0].BotID.String(), answers[1].BotID.String()}
	slices.Sort(got)
	if got[0] != "dev-a" || got[1] != "dev-b" {
		t.Errorf("responders = %v", got)
	}
	// With nobody free, the one with the fewest open tasks answers.
	third := askTeam(t, h, "qa-node", parent.ExecutionID, "dev", "And on production?")
	if third.Refused != nil || third.BotID != "dev-a" {
		t.Errorf("third = %+v %+v", third, third.Refused)
	}
}

func TestTheSameAskReturnsTheSameTaskUntilItFails(t *testing.T) {
	h, parent := channel(t)
	first := askTeam(t, h, "qa-node", parent.ExecutionID, "dev", "Who owns the importer?")
	again := askTeam(t, h, "qa-node", parent.ExecutionID, "dev", "  Who owns the importer?  ")
	if again.Refused != nil || again.Created || again.TaskID != first.TaskID || again.BotID != first.BotID {
		t.Fatalf("again = %+v, first %+v", again, first)
	}
	if asked := h.log.last(domain.EventQuestionAsked); asked.Payload["deduplicated"] != true {
		t.Errorf("asked = %+v", asked.Payload)
	}
	parentTask, _ := h.tasks.Get(context.Background(), parent.Run.TaskID)
	if len(parentTask.Questions()) != 1 {
		t.Errorf("questions = %v", parentTask.Questions())
	}
	// Once it expired unanswered, the same ask makes a new one.
	h.clock.now = t0.Add(QuestionTimeout + time.Second)
	child, _ := h.tasks.Get(context.Background(), first.TaskID)
	if err := child.MissDeadline(h.clock.now, ""); err != nil {
		t.Fatal(err)
	}
	if err := h.tasks.Save(context.Background(), child); err != nil {
		t.Fatal(err)
	}
	retry := askTeam(t, h, "qa-node", parent.ExecutionID, "dev", "Who owns the importer?")
	if retry.Refused != nil || !retry.Created || retry.TaskID == first.TaskID {
		t.Fatalf("retry = %+v %+v", retry, retry.Refused)
	}
	fresh, _ := h.tasks.Get(context.Background(), retry.TaskID)
	if !strings.HasSuffix(fresh.IdempotencyKey().String(), ":2") {
		t.Errorf("key = %s", fresh.IdempotencyKey())
	}
	if same := askTeam(t, h, "qa-node", parent.ExecutionID, "dev", "Who owns the importer?"); same.TaskID != retry.TaskID || same.Created {
		t.Errorf("after the retry = %+v", same)
	}
	// A cancelled one (its bot was stopped) answers nothing either.
	fresh, _ = h.tasks.Get(context.Background(), retry.TaskID)
	if err := fresh.CancelBecause("bot stopped", h.clock.now); err != nil {
		t.Fatal(err)
	}
	_ = h.tasks.Save(context.Background(), fresh)
	third := askTeam(t, h, "qa-node", parent.ExecutionID, "dev", "Who owns the importer?")
	if third.Refused != nil || !third.Created || third.TaskID == retry.TaskID {
		t.Fatalf("after a cancel = %+v %+v", third, third.Refused)
	}
	if again, _ := h.tasks.Get(context.Background(), third.TaskID); !strings.HasSuffix(again.IdempotencyKey().String(), ":3") {
		t.Errorf("key = %s", again.IdempotencyKey())
	}
}

// Only a node that opens the team's store answers: an older one, which
// names no memory team, is never picked, and never takes the task.
func TestAnOldNodeNeverAnswers(t *testing.T) {
	h := newHarness()
	channelNode(t, h, "qa-node")
	switchedNode(t, h, "old-node", false)
	asker := channelBot(t, h, "qa-jefe", []string{"qa"}, &domain.AskSnapshot{Teams: []string{"dev"}}, nil, "qa-node")
	channelBot(t, h, "dev-a", []string{"dev"}, nil, []string{"qa"})
	parent := runOnNode(t, h, asker, "qa-node", "lead-1")
	if refused := askTeam(t, h, "qa-node", parent.ExecutionID, "dev", "Who owns the importer?").Refused; refused == nil || refused.Reason != domain.RefusedTeamUnavailable {
		t.Fatalf("refused = %+v", refused)
	}
	if refused := h.log.last(domain.EventQuestionRefused); refused.Payload["reason"] != domain.RefusedTeamUnavailable {
		t.Errorf("event = %+v", refused.Payload)
	}
	// A node that shares every team's memory answers; the old one and the
	// asker's still take nothing.
	channelNode(t, h, "new-node", domain.AllMemoryTeams)
	answer := askTeam(t, h, "qa-node", parent.ExecutionID, "dev", "Who owns the importer?")
	if answer.Refused != nil || answer.BotID != "dev-a" {
		t.Fatalf("answer = %+v %+v", answer, answer.Refused)
	}
	for _, node := range []domain.NodeID{"old-node", "qa-node"} {
		if _, found, err := NewNextAssignment(h.deps).Execute(context.Background(), node); err != nil || found {
			t.Errorf("%s took it: %v %v", node, found, err)
		}
	}
	// Nor does the control plane run it in process.
	if _, found, err := NewRunNextPending(h.deps).Pick(context.Background()); err != nil || found {
		t.Errorf("picked in process: %v %v", found, err)
	}
	got := nextFor(t, h, "new-node")
	if got.Run == nil || got.Run.TaskID != answer.TaskID {
		t.Fatalf("assignment = %+v", got)
	}
}

// The bot that answers keeps its own tools but asks, delegates, emits and
// subscribes nothing.
func TestTheAnswerRunsWithoutTheNodesTools(t *testing.T) {
	h, parent := channel(t)
	answer := askTeam(t, h, "qa-node", parent.ExecutionID, "dev", "Who owns the importer?")
	got := nextFor(t, h, "dev-node")
	if got.Run == nil || got.Run.TaskID != answer.TaskID {
		t.Fatalf("assignment = %+v", got)
	}
	if got.Run.Delegation != nil || got.Run.Events != nil || len(got.Run.NodeTools()) != 0 {
		t.Errorf("node tools on a question: %+v %+v %v", got.Run.Delegation, got.Run.Events, got.Run.NodeTools())
	}
	tools := got.Run.AllowedTools
	if len(tools) != 1 || tools[0] != "local_read" {
		t.Errorf("tools = %v", tools)
	}
	// Its own budgets and timeout: no caps of the channel's.
	if got.Run.Budgets.MaxModelCallsPerTask() != 20 || got.Run.Budgets.MaxToolCallsPerTask() != 60 || got.Run.Timeout != 900*time.Second || got.Run.MemoryTeam != "dev" {
		t.Errorf("budgets %+v timeout %s memory %q", got.Run.Budgets, got.Run.Timeout, got.Run.MemoryTeam)
	}
	if got.Run.Ask != nil {
		t.Errorf("an ask offer on a question: %+v", got.Run.Ask)
	}
	// A teammate's ordinary task still gets them.
	if _, err := NewSubmitTask(h.deps).Execute(context.Background(), SubmitTaskCommand{BotID: "dev-b", Objective: "Look", IdempotencyKey: "look-1"}); err != nil {
		t.Fatal(err)
	}
	if plain := nextFor(t, h, "dev-node"); plain.Run == nil || plain.Run.Delegation == nil || plain.Run.Events == nil {
		t.Errorf("plain = %+v", plain.Run)
	}
}

func TestAQuestionIsRefusedInTheADRsOrder(t *testing.T) {
	h, parent := channel(t)
	refusal := func(node domain.NodeID, execution domain.ExecutionID, team, question string) string {
		t.Helper()
		answer := askTeam(t, h, node, execution, team, question)
		if answer.Refused == nil {
			t.Fatalf("%s: not refused: %+v", team, answer)
		}
		return answer.Refused.Reason
	}
	if got := refusal("qa-node", parent.ExecutionID, "qa", "Mine?"); got != domain.RefusedOwnTeam {
		t.Errorf("own team: %s", got)
	}
	for _, team := range []string{"marketing", "Not A Team", ""} {
		if got := refusal("qa-node", parent.ExecutionID, team, "Yours?"); got != domain.RefusedNotAllowed {
			t.Errorf("%q: %s", team, got)
		}
	}
	if audit := h.log.audits[len(h.log.audits)-1]; audit.Action != "question.ask" || audit.Outcome != domain.AuditDenied {
		t.Errorf("audit = %+v", audit)
	}

	// No team wins over everything after it.
	loner := channelBot(t, h, "loner", nil, nil, nil, "qa-node")
	lonely := runOnNode(t, h, loner, "qa-node", "loner-1")
	if got := refusal("qa-node", lonely.ExecutionID, "dev", "Anyone?"); got != domain.RefusedNoTeam {
		t.Errorf("no team: %s", got)
	}

	// A bot answering a question asks no third team.
	channelBot(t, h, "dev-0", []string{"dev"}, &domain.AskSnapshot{Teams: []string{"qa"}}, []string{"qa"}, "dev-node")
	q := askTeam(t, h, "qa-node", parent.ExecutionID, "dev", "Who owns the importer?")
	if q.BotID != "dev-0" {
		t.Fatalf("picked %s", q.BotID)
	}
	answering := nextFor(t, h, "dev-node")
	if answering.Run.Ask != nil || slices.Contains(answering.Run.AllowedTools, AskToolName) {
		t.Errorf("a bot answering a question may ask: %+v %v", answering.Run.Ask, answering.Run.AllowedTools)
	}
	if got := refusal("dev-node", answering.ExecutionID, "qa", "And you?"); got != domain.RefusedDepthExceeded {
		t.Errorf("a question asking: %s", got)
	}

	// The asker's rate, then the trace's.
	slow := channelBot(t, h, "qa-slow", []string{"qa"}, &domain.AskSnapshot{Teams: []string{"dev"}, PerMinute: 1}, nil, "qa-node")
	s := runOnNode(t, h, slow, "qa-node", "slow-1")
	if a := askTeam(t, h, "qa-node", s.ExecutionID, "dev", "One"); a.Refused != nil {
		t.Fatalf("first = %+v", a.Refused)
	}
	if got := refusal("qa-node", s.ExecutionID, "dev", "Two"); got != domain.RefusedRateLimited {
		t.Errorf("perMinute: %s", got)
	}
	// A repeat is no new question: it is not rate limited.
	if a := askTeam(t, h, "qa-node", s.ExecutionID, "dev", "One"); a.Refused != nil || a.Created {
		t.Errorf("repeat = %+v %+v", a, a.Refused)
	}
	h.clock.now = t0.Add(2 * time.Minute)
	if a := askTeam(t, h, "qa-node", s.ExecutionID, "dev", "Two"); a.Refused != nil {
		t.Fatalf("a minute later = %+v", a.Refused)
	}
	for _, question := range []string{"Two", "Three"} {
		if a := askTeam(t, h, "qa-node", parent.ExecutionID, "dev", question); a.Refused != nil {
			t.Fatalf("%s = %+v", question, a.Refused)
		}
	}
	if got := refusal("qa-node", parent.ExecutionID, "dev", "Four"); got != domain.RefusedRateLimited {
		t.Errorf("the trace's fourth: %s", got)
	}
}

func TestAnAskersOpenQuestionsAndATeamsQueueAreBounded(t *testing.T) {
	h := newHarness()
	channelNode(t, h, "qa-node")
	channelNode(t, h, "dev-node", "dev")
	channelBot(t, h, "dev-a", []string{"dev"}, nil, []string{"qa"}, "dev-node")
	careful := channelBot(t, h, "qa-careful", []string{"qa"}, &domain.AskSnapshot{Teams: []string{"dev"}, PerMinute: 10, MaxOpen: 1}, nil, "qa-node")
	c := runOnNode(t, h, careful, "qa-node", "careful-1")
	if a := askTeam(t, h, "qa-node", c.ExecutionID, "dev", "One"); a.Refused != nil {
		t.Fatalf("first = %+v", a.Refused)
	}
	if a := askTeam(t, h, "qa-node", c.ExecutionID, "dev", "Two"); a.Refused == nil || a.Refused.Reason != domain.RefusedRateLimited || !strings.Contains(a.Refused.Message, "waiting") {
		t.Errorf("maxOpen = %+v", a.Refused)
	}

	// Eight questions waiting for dev, from eight traces: the ninth is
	// refused as team_busy.
	for i := range 7 {
		asker := channelBot(t, h, "qa-"+string(rune('a'+i)), []string{"qa"}, &domain.AskSnapshot{Teams: []string{"dev"}}, nil, "qa-node")
		e := runOnNode(t, h, asker, "qa-node", "q-"+string(rune('a'+i)))
		if a := askTeam(t, h, "qa-node", e.ExecutionID, "dev", "Question"); a.Refused != nil {
			t.Fatalf("%d = %+v", i, a.Refused)
		}
	}
	late := channelBot(t, h, "qa-late", []string{"qa"}, &domain.AskSnapshot{Teams: []string{"dev"}}, nil, "qa-node")
	l := runOnNode(t, h, late, "qa-node", "late-1")
	if a := askTeam(t, h, "qa-node", l.ExecutionID, "dev", "Question"); a.Refused == nil || a.Refused.Reason != domain.RefusedTeamBusy {
		t.Errorf("ninth = %+v", a.Refused)
	}
}

// A question needs the execution the node holds, running.
func TestAQuestionNeedsTheNodesRunningExecution(t *testing.T) {
	h, parent := channel(t)
	if _, err := NewAskTeam(h.deps).Execute(context.Background(), "dev-node", parent.ExecutionID, AskTeamCommand{Team: "dev", Question: "Who?"}); err == nil {
		t.Error("another node asked for the execution")
	}
	if _, err := NewAskTeam(h.deps).Execute(context.Background(), "qa-node", parent.ExecutionID, AskTeamCommand{Team: "dev", Question: "  "}); err == nil {
		t.Error("an empty question")
	}
	if _, err := NewAskTeam(h.deps).Execute(context.Background(), "qa-node", "exec-404", AskTeamCommand{Team: "dev", Question: "Who?"}); err == nil {
		t.Error("an unknown execution")
	}
}

// last is the newest event of the type, or the zero event.
func (c *collector) last(kind domain.EventType) domain.Event {
	for i := len(c.events) - 1; i >= 0; i-- {
		if c.events[i].Type == kind {
			return c.events[i]
		}
	}
	return domain.Event{}
}
