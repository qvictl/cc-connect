package core

import (
	"errors"
	"strings"
	"testing"
)

type renderedAnswerReceiptSession struct {
	recordingAgentSession
	state      *interactiveState
	err        error
	invalidate bool
}

func (s *renderedAnswerReceiptSession) RespondPermission(id string, result PermissionResult) error {
	_ = s.recordingAgentSession.RespondPermission(id, result)
	if s.invalidate {
		s.state.mu.Lock()
		s.state.stopped = true
		s.state.mu.Unlock()
	}
	return s.err
}

// A successful rendered card must not fall through to the stale branch when
// the upstream echo gate is combined with the local delivery acknowledgement.
func TestRenderedAnswer_LegacyReceiptBranches(t *testing.T) {
	for _, tc := range []struct {
		name       string
		rendered   bool
		err        error
		invalidate bool
		wantEcho   bool
	}{
		{name: "rendered_success", rendered: true},
		{name: "text_success", wantEcho: true},
		{name: "rendered_failure", rendered: true, err: errors.New("receipt failed")},
		{name: "rendered_withdrawn", rendered: true, invalidate: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newTestEngine()
			p := &stubPlatformEngine{n: "test"}
			s := &renderedAnswerReceiptSession{err: tc.err, invalidate: tc.invalidate}
			pending := &pendingPermission{
				RequestID: "question-1", ToolName: "AskUserQuestion",
				ToolInput: map[string]any{"questions": []any{}},
				Questions: testQuestions(), Resolved: make(chan struct{}),
			}
			state := &interactiveState{agentSession: s, platform: p, pending: pending}
			s.state = state
			key := "test:chat:user1"
			e.interactiveStates[key] = state
			msg := &Message{SessionKey: key, UserID: "user1", ReplyCtx: "ctx", CardAnswerRendered: tc.rendered}
			if !e.handlePendingPermission(p, msg, "2", key) || s.calls != 1 {
				t.Fatalf("answer was not submitted exactly once: calls=%d", s.calls)
			}
			sent := p.getSent()
			switch {
			case tc.err != nil:
				if len(sent) != 1 || !strings.Contains(sent[0], "receipt failed") || !pending.retryable {
					t.Fatalf("rendered card hid failure or discarded retry: sent=%v retryable=%v", sent, pending.retryable)
				}
			case tc.invalidate:
				if len(sent) != 1 || sent[0] != e.i18n.T(MsgPermissionStale) {
					t.Fatalf("rendered card hid withdrawal: %v", sent)
				}
			case tc.wantEcho:
				if len(sent) != 1 || !strings.Contains(sent[0], "✅") {
					t.Fatalf("plain answer lost echo: %v", sent)
				}
			default:
				if len(sent) != 0 {
					t.Fatalf("successful rendered answer produced echo or false stale notice: %v", sent)
				}
			}
		})
	}
}

func TestRenderedAnswer_LegacyNextQuestionStillSent(t *testing.T) {
	e := newTestEngine()
	p := &stubPlatformEngine{n: "test"}
	s := &recordingAgentSession{}
	key := "test:chat:user1"
	pending := &pendingPermission{RequestID: "question-1", ToolName: "AskUserQuestion",
		ToolInput: map[string]any{"questions": []any{}}, Questions: testMultiQuestions(), Resolved: make(chan struct{})}
	e.interactiveStates[key] = &interactiveState{agentSession: s, platform: p, pending: pending}
	msg := &Message{SessionKey: key, UserID: "user1", ReplyCtx: "ctx", CardAnswerRendered: true}
	if !e.handlePendingPermission(p, msg, "1", key) || s.calls != 0 || pending.CurrentQuestion != 1 {
		t.Fatal("first answer failed to advance without submitting the whole response")
	}
	sent := p.getSent()
	if len(sent) != 1 || !strings.Contains(sent[0], "Which framework?") || strings.Contains(sent[0], "✅") {
		t.Fatalf("next question missing or answer echoed: %v", sent)
	}
}

func TestRenderedAnswer_AsyncEchoAndPermissionIsolation(t *testing.T) {
	for _, rendered := range []bool{false, true} {
		e := newTestEngine()
		p := &stubPlatformEngine{n: "test"}
		s := &asyncInputSession{active: true}
		key := "test:chat:user1"
		pending := &pendingUserInput{id: "question-1", questions: testQuestions(), answers: make([]string, 1)}
		state := &interactiveState{agentSession: s, platform: p, pendingUserInput: pending}
		e.interactiveStates[key] = state
		msg := &Message{SessionKey: key, ReplyCtx: "ctx", CardAnswerRendered: rendered, IsPermissionResponse: true}
		if _, handled := e.handleAsyncUserInput(p, msg, "perm:old:allow", key, e.sessions); handled {
			t.Fatal("old permission callback was handled as a question answer")
		}
		if pending.answers[0] != "" || len(s.steered) != 0 || len(p.getSent()) != 0 {
			t.Fatal("permission callback altered the pending question")
		}
		msg.IsPermissionResponse = false
		if _, handled := e.handleAsyncUserInput(p, msg, "2", key, e.sessions); !handled || len(s.steered) != 1 {
			t.Fatal("question answer was not steered exactly once")
		}
		want := 1
		if rendered {
			want = 0
		}
		if len(p.getSent()) != want {
			t.Fatalf("rendered=%v: sent=%v", rendered, p.getSent())
		}
	}
}
