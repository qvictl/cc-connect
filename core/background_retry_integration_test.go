package core

import (
	"context"
	"errors"
	"testing"
)

type retryingBackgroundQuestionSession struct {
	*questionSession
	failures int
}

func (s *retryingBackgroundQuestionSession) Alive() bool { return true }

func (s *retryingBackgroundQuestionSession) RespondPermission(id string, result PermissionResult) error {
	if s.failures > 0 {
		s.failures--
		return errors.New("response receipt unavailable")
	}
	return s.questionSession.RespondPermission(id, result)
}

func TestUnsolicitedQuestion_RetryableReceiptDoesNotAdvanceQueue(t *testing.T) {
	q, recorded, _ := newOwnershipQuestionTest(t)
	s := &retryingBackgroundQuestionSession{questionSession: recorded, failures: 1}
	q.agentSession, q.state.agentSession = s, s
	q.ask(backgroundQuestion("first", "Which branch?"))
	first := q.current
	q.ask(backgroundQuestion("second", "Which target?"))
	result := PermissionResult{Behavior: "allow"}
	if accepted, err := respondPendingPermission(q.state, first, result, false, func() {}); accepted || err == nil {
		t.Fatalf("failed receipt accepted=%v, err=%v", accepted, err)
	}
	select {
	case <-first.Resolved:
	default:
		t.Fatal("failed receipt did not release the foreground reader")
	}
	select {
	case <-q.answered():
		t.Fatal("retryable receipt advanced the background question")
	default:
	}
	if q.current != first || q.state.pending != first || len(q.queued) != 1 {
		t.Fatal("failed receipt lost ownership of the first question")
	}
	if accepted, err := respondPendingPermission(q.state, first, result, false, func() {}); !accepted || err != nil {
		t.Fatalf("retry accepted=%v, err=%v", accepted, err)
	}
	select {
	case <-q.answered():
	default:
		t.Fatal("successful retry did not complete the background question")
	}
	q.next(context.Background())
	if q.current == nil || q.current.RequestID != "second" || q.state.pending != q.current {
		t.Fatal("second question not shown after successful retry")
	}
	q.withdraw()
	got := recorded.waitForResponses(t, 2)
	if got[0].id != "first" || got[0].result.Behavior != "allow" || got[1].id != "second" || got[1].result.Behavior != "deny" {
		t.Fatalf("unexpected responses: %+v", got)
	}
}

func TestUnsolicitedQuestion_WaitsThroughOccupiedSlotRetry(t *testing.T) {
	q, recorded, _ := newOwnershipQuestionTest(t)
	s := &retryingBackgroundQuestionSession{questionSession: recorded, failures: 1}
	q.agentSession, q.state.agentSession = s, s
	foreground := &pendingPermission{RequestID: "foreground", Resolved: make(chan struct{})}
	q.state.pending = foreground
	q.ask(backgroundQuestion("background", "Which branch?"))
	result := PermissionResult{Behavior: "allow"}
	if _, err := respondPendingPermission(q.state, foreground, result, false, func() {}); err == nil {
		t.Fatal("expected failed foreground receipt")
	}
	select {
	case <-q.answered():
		t.Fatal("background queue would spin on a released foreground reader")
	default:
	}
	if q.state.pending != foreground || q.current != nil {
		t.Fatal("background question replaced the retryable foreground slot")
	}
	if _, err := respondPendingPermission(q.state, foreground, result, false, func() {}); err != nil {
		t.Fatal(err)
	}
	q.next(context.Background())
	if q.current == nil || q.current.RequestID != "background" {
		t.Fatal("background question not shown when foreground completed")
	}
	q.withdraw()
	recorded.waitForResponses(t, 2)
}

func TestUnsolicitedQuestion_CancelAfterFailedReceiptWithdrawsOnce(t *testing.T) {
	q, recorded, _ := newOwnershipQuestionTest(t)
	s := &retryingBackgroundQuestionSession{questionSession: recorded, failures: 1}
	q.agentSession, q.state.agentSession = s, s
	q.ask(backgroundQuestion("background", "Which branch?"))
	pending := q.current
	if _, err := respondPendingPermission(q.state, pending, PermissionResult{Behavior: "allow"}, false, func() {}); err == nil {
		t.Fatal("expected failed receipt")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	q.next(ctx)
	q.withdraw()
	q.withdraw()
	got := recorded.waitForResponses(t, 1)
	if len(got) != 1 || got[0].id != "background" || got[0].result.Behavior != "deny" {
		t.Fatalf("unexpected withdrawal responses: %+v", got)
	}
	select {
	case <-pending.completedSignal():
	default:
		t.Fatal("withdrawal did not complete the pending question")
	}
}

type blockedFailingQuestionSession struct {
	*questionSession
	entered chan struct{}
	release chan struct{}
}

func (s *blockedFailingQuestionSession) Alive() bool { return true }

func (s *blockedFailingQuestionSession) RespondPermission(id string, result PermissionResult) error {
	if result.Behavior == "allow" {
		close(s.entered)
		<-s.release
		return errors.New("response receipt unavailable")
	}
	return s.questionSession.RespondPermission(id, result)
}

func TestUnsolicitedQuestion_WithdrawDuringFailedAnswerDoesNotOrphanRetry(t *testing.T) {
	q, recorded, _ := newOwnershipQuestionTest(t)
	s := &blockedFailingQuestionSession{questionSession: recorded, entered: make(chan struct{}), release: make(chan struct{})}
	q.agentSession, q.state.agentSession = s, s
	q.ask(backgroundQuestion("background", "Which branch?"))
	pending := q.current
	done := make(chan error, 1)
	go func() {
		_, err := respondPendingPermission(q.state, pending, PermissionResult{Behavior: "allow"}, false, func() {})
		done <- err
	}()
	<-s.entered
	q.withdraw()
	close(s.release)
	if err := <-done; err == nil {
		t.Fatal("expected failed receipt")
	}
	got := recorded.waitForResponses(t, 1)
	if len(got) != 1 || got[0].id != "background" || got[0].result.Behavior != "deny" {
		t.Fatalf("unexpected deferred withdrawal: %+v", got)
	}
	q.state.mu.Lock()
	orphan := q.state.pending == pending || pending.retryable
	q.state.mu.Unlock()
	if orphan {
		t.Fatal("failed answer retained a retry after its background reader exited")
	}
	select {
	case <-pending.completedSignal():
	default:
		t.Fatal("deferred withdrawal did not finish the question")
	}
}
