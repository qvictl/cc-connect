package core

import (
	"context"
	"sync"
	"testing"
	"time"
)

func newOwnershipQuestionTest(t *testing.T) (*unsolicitedQuestions, *questionSession, *stubPlatformEngine) {
	t.Helper()
	p := &stubPlatformEngine{n: "test"}
	e := NewEngine("test", &stubAgent{}, []Platform{p}, "", LangEnglish)
	t.Cleanup(func() {
		if err := e.Stop(); err != nil {
			t.Errorf("stop engine: %v", err)
		}
	})
	session := &questionSession{}
	state := &interactiveState{platform: p, replyCtx: "ctx", agentSession: session}
	return e.newUnsolicitedQuestions(state, session), session, p
}

func TestUnsolicitedQuestion_WithdrawAfterSlotReplacement(t *testing.T) {
	q, session, _ := newOwnershipQuestionTest(t)
	q.ask(backgroundQuestion("background", "Which branch?"))
	pending := q.current
	foreground := &pendingPermission{RequestID: "foreground", Resolved: make(chan struct{})}
	q.state.mu.Lock()
	q.state.pending = foreground
	q.state.mu.Unlock()
	q.withdraw()
	got := session.waitForResponses(t, 1)
	if got[0].id != "background" || got[0].result.Behavior != "deny" {
		t.Fatalf("wrong withdrawal: %v", got)
	}
	if q.state.pending != foreground {
		t.Fatal("withdrawal changed foreground ownership")
	}
	select {
	case <-pending.Resolved:
	default:
		t.Fatal("old question waiter not released")
	}
}

func TestUnsolicitedQuestion_WaitsForOccupiedSlot(t *testing.T) {
	q, session, p := newOwnershipQuestionTest(t)
	foreground := &pendingPermission{RequestID: "foreground", Resolved: make(chan struct{})}
	q.state.pending = foreground
	q.ask(backgroundQuestion("background", "Which branch?"))
	if q.state.pending != foreground {
		t.Fatal("background question replaced foreground permission")
	}
	if len(p.getSent()) != 0 || len(session.recorded()) != 0 {
		t.Fatal("question shown or denied before foreground resolved")
	}
	if q.answered() != foreground.completedSignal() {
		t.Fatal("reader cannot wake when foreground releases slot")
	}
	if !claimPending(q.state, foreground) {
		t.Fatal("foreground cannot claim its response")
	}
	foreground.resolve()
	q.next(context.Background())
	if q.state.pending == nil || q.state.pending.RequestID != "background" {
		t.Fatal("queued question was not shown")
	}
	q.withdraw()
	session.waitForResponses(t, 1)
}

func TestUnsolicitedQuestion_WithdrawQueuedBehindForeground(t *testing.T) {
	q, session, _ := newOwnershipQuestionTest(t)
	foreground := &pendingPermission{RequestID: "foreground", Resolved: make(chan struct{})}
	q.state.pending = foreground
	q.ask(backgroundQuestion("background", "Which branch?"))
	q.withdraw()
	got := session.waitForResponses(t, 1)
	if got[0].id != "background" || got[0].result.Behavior != "deny" {
		t.Fatalf("wrong withdrawal: %v", got)
	}
	if q.state.pending != foreground {
		t.Fatal("withdrawal changed foreground ownership")
	}
	select {
	case <-foreground.Resolved:
		t.Fatal("withdrawal resolved foreground permission")
	default:
	}
}

// The answer owns its response before the adapter call completes. Losing the
// shared slot in that interval must not let withdrawal send a second response.
func TestUnsolicitedQuestion_WithdrawDoesNotStealClaimedAnswer(t *testing.T) {
	q, session, _ := newOwnershipQuestionTest(t)
	q.ask(backgroundQuestion("background", "Which branch?"))
	pending := q.current
	if !claimPending(q.state, pending) {
		t.Fatal("answer could not claim request")
	}
	foreground := &pendingPermission{RequestID: "foreground", Resolved: make(chan struct{})}
	q.state.pending = foreground
	q.withdraw()
	if q.state.pending != foreground {
		t.Fatal("withdrawal changed foreground ownership")
	}
	select {
	case <-pending.Resolved:
		t.Fatal("withdrawal stole the in-flight answer")
	default:
	}
	if len(session.recorded()) != 0 {
		t.Fatal("withdrawal denied the in-flight answer")
	}
}

type blockedQuestionPlatform struct {
	stubPlatformEngine
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (p *blockedQuestionPlatform) Reply(ctx context.Context, replyCtx any, content string) error {
	p.once.Do(func() { close(p.entered) })
	<-p.release // deliberately emulate an adapter that ignores cancellation
	return p.stubPlatformEngine.Reply(ctx, replyCtx, content)
}

func (p *blockedQuestionPlatform) Send(ctx context.Context, replyCtx any, content string) error {
	return p.Reply(ctx, replyCtx, content)
}

func TestUnsolicitedReader_BoundedStopWithdrawsReplacedQuestion(t *testing.T) {
	p := &blockedQuestionPlatform{stubPlatformEngine: stubPlatformEngine{n: "test"}, entered: make(chan struct{}), release: make(chan struct{})}
	e := NewEngine("test", &stubAgent{}, []Platform{p}, "", LangEnglish)
	var release sync.Once
	t.Cleanup(func() {
		release.Do(func() { close(p.release) })
		if err := e.Stop(); err != nil {
			t.Errorf("stop engine: %v", err)
		}
	})
	agentSession := &questionSession{controllableAgentSession: controllableAgentSession{sessionID: "blocked-question", alive: true, events: make(chan Event, 8), closed: make(chan struct{})}}
	state := &interactiveState{agentSession: agentSession, platform: p, replyCtx: "ctx"}
	session := e.sessions.GetOrCreateActive(questionKey)
	e.startUnsolicitedReader(state, session, e.sessions, questionKey, "")
	state.mu.Lock()
	done := state.unsolicitedDone
	state.mu.Unlock()
	agentSession.events <- backgroundQuestion("background", "Which branch?")
	select {
	case <-p.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("reader did not enter question send")
	}
	e.stopUnsolicitedReader(state)
	foreground := &pendingPermission{RequestID: "foreground", Resolved: make(chan struct{})}
	state.mu.Lock()
	resync := state.eventsNeedResync
	state.pending = foreground // the next turn now owns the shared slot
	state.mu.Unlock()
	if !resync {
		t.Fatal("stop did not hit its bounded handoff")
	}
	release.Do(func() { close(p.release) })
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("old reader did not exit after send unblocked")
	}
	got := agentSession.waitForResponses(t, 1)
	if got[0].id != "background" || got[0].result.Behavior != "deny" {
		t.Fatalf("wrong withdrawal: %v", got)
	}
	state.mu.Lock()
	still := state.pending
	state.mu.Unlock()
	if still != foreground {
		t.Fatal("old reader changed foreground ownership")
	}
}

func TestUnsolicitedQuestion_CanceledResolutionStillWithdraws(t *testing.T) {
	q, session, _ := newOwnershipQuestionTest(t)
	q.ask(backgroundQuestion("background", "Which branch?"))
	pending := q.current
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	q.state.mu.Lock()
	q.state.pending = nil // session cleanup released the slot, not an answer
	q.state.mu.Unlock()
	pending.resolve()
	q.next(ctx)
	q.withdraw()
	got := session.waitForResponses(t, 1)
	if got[0].id != "background" || got[0].result.Behavior != "deny" {
		t.Fatalf("wrong withdrawal: %v", got)
	}
}
