package core

import (
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestGoalStopRejectsLateActiveEvents(t *testing.T) {
	for _, replacement := range []bool{false, true} {
		t.Run(map[bool]string{false: "captured", true: "replacement"}[replacement], func(t *testing.T) {
			env := newCUJEnv(t)
			t.Cleanup(func() { _ = env.engine.Stop() })
			key := "test:late-goal"
			stored := env.engine.sessions.GetOrCreateActive(key)
			stored.SetResumeGoal(true)
			oldSession := newControllableSession("old-goal")
			oldState := &interactiveState{agentSession: oldSession, platform: env.plat,
				replyCtx: "ctx", busySession: stored, goalSessionStore: env.engine.sessions}
			env.engine.interactiveStates[key] = oldState
			active := Event{Type: EventGoal, Goal: &Goal{Status: "active"}}
			oldSession.cancelTurnFn = func() error {
				// The foreground reader can receive an old notification while
				// CancelTurn waits. It must not restore the revoked intent.
				env.engine.relayGoalEvent(oldState, stored, env.engine.sessions, active)
				if stored.GetResumeGoal() {
					t.Error("late active event revived the captured Goal")
				}
				if replacement {
					newState := &interactiveState{agentSession: newControllableSession("new-goal"), platform: env.plat}
					env.engine.interactiveMu.Lock()
					env.engine.interactiveStates[key] = newState
					env.engine.interactiveMu.Unlock()
					stored.SetResumeGoal(true)
					env.engine.sessions.Save()
					// A late paused event from the old process must not revoke
					// the replacement's independently established intent either.
					env.engine.relayGoalEvent(oldState, stored, env.engine.sessions, Event{Type: EventGoal})
				}
				return errors.New("interrupt failed")
			}
			env.engine.stopInteractiveSession(key, env.plat, "ctx")
			env.waitFor("old Goal closes", time.Second, func() bool { return oldSession.closeFinished.Load() })
			oldState.mu.Lock()
			oldState.turnCancelled = false // terminal results may reset this flag
			oldState.mu.Unlock()
			env.engine.relayGoalEvent(oldState, stored, env.engine.sessions, active)
			if stored.GetResumeGoal() != replacement {
				t.Fatal("old event changed the intended recovery state")
			}
			reloaded := NewSessionManager(filepath.Join(env.tempDir, "sessions.json"))
			if reloaded.GetOrCreateActive(key).GetResumeGoal() != replacement {
				t.Fatal("incorrect recovery state persisted")
			}
		})
	}
}

func TestGoalExplicitResumeReopensStoppedRecovery(t *testing.T) {
	env := newCUJEnv(t)
	t.Cleanup(func() { _ = env.engine.Stop() })
	s := &goalTestSession{cujAgentSession: newCUJAgentSession()}
	env.engine.agent = &goalTestAgent{session: s}
	msg := &Message{SessionKey: "test:goal-user", ReplyCtx: "ctx"}
	env.engine.cmdGoal(env.plat, msg, []string{"set", "Work"})
	env.engine.cmdStop(env.plat, msg)
	env.engine.interactiveMu.Lock()
	state := env.engine.interactiveStates[msg.SessionKey]
	env.engine.interactiveMu.Unlock()
	if state == nil {
		t.Fatal("successful pause should retain the session")
	}
	stored := env.engine.sessions.GetOrCreateActive(msg.SessionKey)
	state.mu.Lock()
	state.turnCancelled = false
	state.mu.Unlock()
	env.engine.relayGoalEvent(state, stored, env.engine.sessions, Event{Type: EventGoal, Goal: &Goal{Status: "active"}})
	if stored.GetResumeGoal() {
		t.Fatal("resetting turnCancelled reopened old Goal events")
	}
	env.engine.cmdGoal(env.plat, msg, nil)
	if stored.GetResumeGoal() {
		t.Fatal("show revived a stopped Goal")
	}
	env.engine.cmdGoal(env.plat, msg, []string{"resume"})
	if !stored.GetResumeGoal() {
		t.Fatal("explicit resume did not restore recovery")
	}
	env.engine.relayGoalEvent(state, stored, env.engine.sessions, Event{Type: EventGoal, Goal: &Goal{Status: "completed"}})
	if stored.GetResumeGoal() {
		t.Fatal("legitimate completion after resume was ignored")
	}
}
