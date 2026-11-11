package application

import (
	"context"
	"slices"

	"github.com/underpass-ai/axlr-bots/domain"
)

// GetQuestion is a node reading where a question its execution's task
// asked stands, from a node with the team channel on. A node sees only the questions of the task whose
// execution it holds: a question_answer task asked none, so it reads none.
type GetQuestion struct {
	deps Dependencies
}

// NewGetQuestion wires the use case.
func NewGetQuestion(deps Dependencies) GetQuestion { return GetQuestion{deps: deps} }

// Execute answers the question's view.
func (uc GetQuestion) Execute(ctx context.Context, node domain.NodeID, executionID domain.ExecutionID, question domain.TaskID) (QuestionView, error) {
	execution, err := runningExecutionOf(ctx, uc.deps, node, executionID, "read a question")
	if err != nil {
		return QuestionView{}, err
	}
	if _, err := channelNodeOf(ctx, uc.deps, node, "read a question"); err != nil {
		return QuestionView{}, err
	}
	parent, err := uc.deps.Tasks.Get(ctx, execution.TaskID())
	if err != nil {
		return QuestionView{}, err
	}
	if !slices.Contains(parent.Questions(), question) {
		// Another task's question is not this node's to read.
		return QuestionView{}, domain.NotFoundError{Kind: "question", ID: question.String()}
	}
	child, err := uc.deps.Tasks.Get(ctx, question)
	if err != nil {
		return QuestionView{}, err
	}
	return readQuestionView(ctx, uc.deps, child)
}
