package core

import (
	"fmt"
	"sync/atomic"
	"testing"
	"time"
)

type cancelCloseCountingSession struct {
	*controllableAgentSession
	closeCalls atomic.Int32
}

func (s *cancelCloseCountingSession) Close() error {
	if s.closeCalls.Add(1) != 1 {
		return fmt.Errorf("TEST_ONLY duplicate close")
	}
	return s.controllableAgentSession.Close()
}

func TestCmdCancel_CloseOwnershipAcrossSuccessAndFailure(t *testing.T) {
	for _, failed := range []bool{false, true} {
		t.Run(fmt.Sprint(failed), func(t *testing.T) {
			base := newControllableSession("TEST_ONLY_cancel_close")
			base.cancelTurnFn = func() error {
				if failed { return fmt.Errorf("TEST_ONLY cancel failure") }
				return nil
			}
			sess := &cancelCloseCountingSession{controllableAgentSession: base}
			p := &stubPlatformEngine{n: "test"}
			e := NewEngine("test", &stubAgent{}, []Platform{p}, "", LangEnglish)
			t.Cleanup(func() {
				if err := e.Stop(); err != nil {
					t.Errorf("Stop: %v", err)
				}
			})
			const key = "test:TEST_ONLY_cancel_close"
			state := &interactiveState{agentSession: sess, platform: p, replyCtx: "ctx"}
			e.interactiveStates[key] = state
			e.cmdCancel(p, &Message{SessionKey: key, ReplyCtx: "ctx"})
			select {
			case <-base.closed:
			case <-time.After(2*time.Second): t.Fatal("captured session was not closed")
			}
			// Repeated conditional cleanup must remain a no-op after transfer.
			e.cleanupInteractiveState(key, state)
			if sess.closeCalls.Load()!=1 { t.Fatal("session closed more than once") }
			state.mu.Lock()
			remaining := state.agentSession
			state.mu.Unlock()
			if remaining!=nil { t.Fatal("captured state retained close ownership") }
		})
	}
}

func TestCmdCancel_ReplacementKeepsItsCloseOwnership(t *testing.T) {
	for _, failed := range []bool{false, true} {
		t.Run(fmt.Sprint(failed), func(t *testing.T) {
			p := &stubPlatformEngine{n: "test"}
			e := NewEngine("test", &stubAgent{}, []Platform{p}, "", LangEnglish)
			t.Cleanup(func() {
				if err := e.Stop(); err != nil {
					t.Errorf("Stop: %v", err)
				}
			})
			old := &cancelCloseCountingSession{controllableAgentSession: newControllableSession("TEST_ONLY_old")}
			replacement := &cancelCloseCountingSession{controllableAgentSession: newControllableSession("TEST_ONLY_new")}
			const key = "test:TEST_ONLY_replacement"
			next := &interactiveState{agentSession: replacement, platform: p}
			e.interactiveStates[key] = &interactiveState{agentSession: old, platform: p}
			old.cancelTurnFn = func() error {
				e.interactiveMu.Lock()
				e.interactiveStates[key] = next
				e.interactiveMu.Unlock()
				if failed { return fmt.Errorf("TEST_ONLY interrupted replacement") }
				return nil
			}
			e.cmdCancel(p, &Message{SessionKey: key})
			select {
			case <-old.closed:
			case <-time.After(2*time.Second): t.Fatal("old session not reaped")
			}
			e.interactiveMu.Lock()
			current := e.interactiveStates[key]
			e.interactiveMu.Unlock()
			if current!=next || replacement.closeCalls.Load()!=0 || old.closeCalls.Load()!=1 {
				t.Fatal("replacement identity or close ownership lost")
			}
		})
	}
}
