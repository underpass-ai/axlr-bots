package nodeagent

import (
	"sync"

	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

// executionSubjects remembers who each running execution is about (its
// bot, task and correlation id, which the assignment carries), so the
// helpers that know only the execution id log with all of them. A nil
// one (an agent used without Run) remembers nothing.
type executionSubjects struct {
	mu   sync.Mutex
	byID map[domain.ExecutionID]application.LogSubject
}

func newExecutionSubjects() *executionSubjects {
	return &executionSubjects{byID: map[domain.ExecutionID]application.LogSubject{}}
}

func (s *executionSubjects) add(execution domain.ExecutionID, subject application.LogSubject) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.byID[execution] = subject
}

func (s *executionSubjects) remove(execution domain.ExecutionID) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.byID, execution)
}

func (s *executionSubjects) of(execution domain.ExecutionID) application.LogSubject {
	if s == nil {
		return application.LogSubject{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.byID[execution]
}
