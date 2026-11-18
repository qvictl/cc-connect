package core

import (
	"testing"
	"time"
)

func TestInteractiveEventLoopStartsAfterTeardownClaim(t *testing.T) {
	p := &stubPlatformEngine{n: "test"}
	e := NewEngine("test", &stubAgent{}, []Platform{p}, "", LangEnglish)
	key := "test:late-loop"
	session := e.sessions.GetOrCreateActive(key)
	// cmdStop owns and clears the session before the event-loop goroutine
	// starts. Reproduce that ordering without relying on scheduler timing.
	state := &interactiveState{platform: p, replyCtx: "ctx"}
	state.markStopped()
	done := make(chan struct{})
	go func() {
		e.processInteractiveEvents(state, session, e.sessions, key, "", time.Now(), nil, nil, "ctx", 0)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("event loop did not return after teardown claimed the session")
	}
	if sent := p.getSent(); len(sent) != 0 {
		t.Fatalf("stopped event loop sent messages: %v", sent)
	}
}
