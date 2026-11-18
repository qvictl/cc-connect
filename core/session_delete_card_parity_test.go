package core

import (
	"errors"
	"sync"
	"testing"
	"time"
)

// Card navigation reaches the same activation reservation as text /switch.
// It must not revive a deleted ID even when ListSessions returns a stale row.
func TestDeleteSingleSessionReply_BlocksConcurrentCardSwitch(t *testing.T) {
	for _, fails := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "failure"}[fails], func(t *testing.T) {
			d := &blockingSessionDeleter{entered: make(chan struct{}), release: make(chan struct{})}
			if fails {
				d.err = errors.New("delete failed")
			}
			a := &blockingDeleteListAgent{stubListAgent: stubListAgent{sessions: []AgentSessionInfo{{ID: "target", Summary: "Target"}}}, blockingSessionDeleter: d}
			e := NewEngine("test", a, nil, "", LangEnglish)
			e.sessions.GetOrCreateActive("test:other")
			var once sync.Once
			release := func() { once.Do(func() { close(d.release) }) }
			t.Cleanup(release)
			deleted := make(chan string, 1)
			go func() {
				deleted <- e.deleteSingleSessionReply(&Message{SessionKey: "test:caller"}, d, &AgentSessionInfo{ID: "target"})
			}()
			select {
			case <-d.entered:
			case <-time.After(2 * time.Second):
				t.Fatal("deletion did not start")
			}
			switched := make(chan struct{})
			go func() { e.executeCardAction("/switch", "target", "test:other"); close(switched) }()
			select {
			case <-switched:
				t.Fatal("card activation did not wait for deletion")
			case <-time.After(50 * time.Millisecond):
			}
			release()
			select {
			case <-deleted:
			case <-time.After(2 * time.Second):
				t.Fatal("deletion did not finish")
			}
			select {
			case <-switched:
			case <-time.After(2 * time.Second):
				t.Fatal("card activation remained blocked")
			}
			got := e.sessions.GetOrCreateActive("test:other").GetAgentSessionID()
			if (got == "target") != fails {
				t.Fatalf("card target = %q after deletion failure=%v", got, fails)
			}
		})
	}
}
