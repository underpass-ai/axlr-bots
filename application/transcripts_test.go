package application

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/underpass-ai/axlr-bots/domain"
)

// memTranscripts is a TranscriptStore in memory; fail makes every call
// fail.
type memTranscripts struct {
	rows []TranscriptRecord
	data map[string][]byte
	fail error
	// nilList answers a nil list.
	nilList bool
}

func (m *memTranscripts) SaveTranscript(_ context.Context, record TranscriptRecord, content []byte) (TranscriptRecord, error) {
	if m.fail != nil {
		return TranscriptRecord{}, m.fail
	}
	if m.data == nil {
		m.data = map[string][]byte{}
	}
	record.Seq = int64(len(m.rows) + 1)
	m.rows = append(m.rows, record)
	m.data[record.Execution.String()+"/"+record.Session] = content
	return record, nil
}

func (m *memTranscripts) LoadTranscript(_ context.Context, execution domain.ExecutionID, session string) (TranscriptRecord, []byte, error) {
	if m.fail != nil {
		return TranscriptRecord{}, nil, m.fail
	}
	for _, row := range m.rows {
		if row.Execution == execution && row.Session == session {
			return row, m.data[execution.String()+"/"+session], nil
		}
	}
	return TranscriptRecord{}, nil, domain.NotFoundError{Kind: "transcript", ID: execution.String()}
}

func (m *memTranscripts) ListTranscripts(_ context.Context, after int64, limit int) ([]TranscriptRecord, error) {
	if m.fail != nil {
		return nil, m.fail
	}
	if m.nilList {
		return nil, nil
	}
	out := []TranscriptRecord{}
	for _, row := range m.rows {
		if row.Seq > after && len(out) < limit {
			out = append(out, row)
		}
	}
	return out, nil
}

func upload(content string) TranscriptUpload {
	return TranscriptUpload{Session: "sess-1", Content: []byte(content), SHA256: digest(content)}
}

func TestANodeUploadsTheWholeTranscriptOfItsExecution(t *testing.T) {
	h := newHarness()
	store := &memTranscripts{}
	h.deps.Transcripts = store
	ctx := context.Background()
	node, execution := runningOnNode(t, h)
	content := `{"messages":[{"role":"tool","content":"` + strings.Repeat("y", 50000) + `"}]}`
	record, err := NewUploadTranscript(h.deps).Execute(ctx, node.ID(), execution, upload(content))
	if err != nil {
		t.Fatal(err)
	}
	if record.Seq != 1 || record.Execution != execution || record.Bot == "" || record.Task == "" || record.Size != int64(len(content)) || record.SHA256 != digest(content) || record.ReceivedAt.IsZero() {
		t.Fatalf("record = %+v", record)
	}
	if audits := auditsOf(h, "transcript.upload"); len(audits) != 1 || audits[0].Outcome != domain.AuditOK {
		t.Fatalf("audits = %+v", audits)
	}
	got, data, err := NewGetTranscript(h.deps).Execute(ctx, execution, "sess-1")
	if err != nil || string(data) != content || got.Seq != 1 {
		t.Fatalf("get: %+v %d %v", got, len(data), err)
	}
	list, err := NewListTranscripts(h.deps).Execute(ctx, 0, 0)
	if err != nil || len(list) != 1 {
		t.Fatalf("list: %+v %v", list, err)
	}
	if list, err := NewListTranscripts(h.deps).Execute(ctx, 1, 9999); err != nil || len(list) != 0 {
		t.Fatalf("after: %+v %v", list, err)
	}
	// The execution ended: nothing more is taken for it.
	report := ExecutionReport{Outcome: domain.ExecutionCompleted, Result: domain.Result{Answer: "done"}}
	if _, err := NewReportExecution(h.deps).Execute(ctx, node.ID(), execution, report); err != nil {
		t.Fatal(err)
	}
	if _, err := NewUploadTranscript(h.deps).Execute(ctx, node.ID(), execution, upload("{}")); err == nil {
		t.Error("an upload for an ended execution was taken")
	}
}

func TestATranscriptIsCheckedBeforeItIsKept(t *testing.T) {
	h := newHarness()
	store := &memTranscripts{}
	h.deps.Transcripts = store
	ctx := context.Background()
	node, execution := runningOnNode(t, h)
	uc := NewUploadTranscript(h.deps)
	bad := upload("{}")
	bad.SHA256 = digest("other")
	cases := map[string]struct {
		node   domain.NodeID
		exec   domain.ExecutionID
		upload TranscriptUpload
	}{
		"wrong hash":    {node.ID(), execution, bad},
		"bad session":   {node.ID(), execution, TranscriptUpload{Session: "../x", Content: []byte("{}"), SHA256: digest("{}")}},
		"empty session": {node.ID(), execution, TranscriptUpload{Content: []byte("{}"), SHA256: digest("{}")}},
		"long session":  {node.ID(), execution, TranscriptUpload{Session: strings.Repeat("a", 65), Content: []byte("{}"), SHA256: digest("{}")}},
		"too big":       {node.ID(), execution, TranscriptUpload{Session: "s", Content: make([]byte, MaxTranscriptBytes+1)}},
		"other node":    {"web-9", execution, upload("{}")},
		"no execution":  {node.ID(), "exec-missing", upload("{}")},
	}
	for name, c := range cases {
		if _, err := uc.Execute(ctx, c.node, c.exec, c.upload); err == nil {
			t.Errorf("%s: taken", name)
		}
	}
	if len(store.rows) != 0 {
		t.Fatalf("rows = %+v", store.rows)
	}
	store.fail = errBoom
	if _, err := uc.Execute(ctx, node.ID(), execution, upload("{}")); !errors.Is(err, errBoom) {
		t.Errorf("store failure: %v", err)
	}
	if _, _, err := NewGetTranscript(h.deps).Execute(ctx, execution, "sess-1"); !errors.Is(err, errBoom) {
		t.Errorf("get failure: %v", err)
	}
	if _, err := NewListTranscripts(h.deps).Execute(ctx, 0, 1); !errors.Is(err, errBoom) {
		t.Errorf("list failure: %v", err)
	}
	store.fail = nil
	store.nilList = true
	if list, err := NewListTranscripts(h.deps).Execute(ctx, 0, 1); err != nil || list == nil {
		t.Errorf("nil list: %v %v", list, err)
	}
	if _, err := NewListTranscripts(h.deps).Execute(ctx, -1, 1); err == nil {
		t.Error("a negative cursor was taken")
	}
	if _, _, err := NewGetTranscript(h.deps).Execute(ctx, execution, "a b"); err == nil {
		t.Error("a bad session was read")
	}
	if audits := auditsOf(h, "transcript.upload"); len(audits) == 0 || audits[0].Outcome != domain.AuditDenied {
		t.Errorf("audits = %+v", audits)
	}
}

func TestAControlPlaneWithoutTranscriptsRefuses(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	node, execution := runningOnNode(t, h)
	if _, err := NewUploadTranscript(h.deps).Execute(ctx, node.ID(), execution, upload("{}")); err == nil {
		t.Error("upload taken without a store")
	}
	if _, err := NewListTranscripts(h.deps).Execute(ctx, 0, 0); err == nil {
		t.Error("list without a store")
	}
	if _, _, err := NewGetTranscript(h.deps).Execute(ctx, execution, "sess-1"); err == nil {
		t.Error("get without a store")
	}
}
