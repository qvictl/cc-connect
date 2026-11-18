package core

import (
	"errors"
	"sync"
	"testing"
)

type boundRetrySession struct {
	stubAgentSession
	calls int
}

func (s *boundRetrySession) RespondPermission(string, PermissionResult) error {
	s.calls++
	if s.calls == 1 {
		return errors.New("receipt failed")
	}
	return nil
}

func TestBoundPermissionRejectsStaleMalformedAndQuestionCallbacks(t *testing.T) {
	for _, input := range []string{"perm:old:allow_all", "perm:current:unknown", "perm::allow", "perm:current", "perm:current:allow:extra"} {
		e := newTestEngine()
		agent := &recordingAgentSession{}
		pending := &pendingPermission{RequestID: "reused-agent-id", CallbackID: "current", Resolved: make(chan struct{})}
		state := &interactiveState{agentSession: agent, pending: pending}
		e.interactiveStates["chat"] = state
		if !e.handlePendingPermission(&stubPlatformEngine{n: "card"}, &Message{SessionKey: "chat", IsPermissionResponse: true}, input, "chat") {
			t.Fatal("invalid callback escaped into ordinary input")
		}
		if agent.calls != 0 || state.pending != pending || state.approveAll {
			t.Fatal("invalid callback changed the pending request")
		}
	}
	e := newTestEngine()
	agent := &recordingAgentSession{}
	pending := &pendingPermission{CallbackID: "question", Questions: []UserQuestion{{Question: "Choose"}}, Resolved: make(chan struct{})}
	e.interactiveStates["chat"] = &interactiveState{agentSession: agent, pending: pending}
	if !e.handlePendingPermission(&stubPlatformEngine{n: "card"}, &Message{SessionKey: "chat", IsPermissionResponse: true}, "perm:question:allow", "chat") || agent.calls != 0 || pending.Answers != nil {
		t.Fatal("permission callback answered an AskUserQuestion")
	}
	delete(e.interactiveStates, "chat")
	if !e.handlePendingPermission(&stubPlatformEngine{n: "card"}, &Message{SessionKey: "chat", IsPermissionResponse: true}, "perm:question:allow", "chat") {
		t.Fatal("callback with no state escaped into ordinary input")
	}
}

func TestBoundPermissionFindsOnlyMatchingCronScope(t *testing.T) {
	e := newTestEngine()
	agents := make(map[string]*recordingAgentSession)
	for key, binding := range map[string]string{"chat": "foreground", "chat#cron:first": "first", "chat#cron:second": "second", "other#cron:third": "elsewhere"} {
		agent := &recordingAgentSession{}
		agents[key] = agent
		e.interactiveStates[key] = &interactiveState{agentSession: agent, pending: &pendingPermission{RequestID: key, CallbackID: binding, Resolved: make(chan struct{})}}
	}
	p := &stubPlatformEngine{n: "card"}
	msg := &Message{SessionKey: "chat", IsPermissionResponse: true}
	e.handlePendingPermission(p, msg, "perm:elsewhere:allow", "chat")
	e.handlePendingPermission(p, msg, "perm:second:deny", "chat")
	for key, agent := range agents {
		want := 0
		if key == "chat#cron:second" {
			want = 1
		}
		if agent.calls != want {
			t.Fatalf("scope %s received %d responses, want %d", key, agent.calls, want)
		}
	}
	if agents["chat#cron:second"].lastResult.Behavior != "deny" {
		t.Fatal("matching cron decision changed")
	}
}

func TestBoundPermissionRetryRetainsBindingAndRepeatedClickIsIgnored(t *testing.T) {
	e := newTestEngine()
	agent := &boundRetrySession{}
	pending := &pendingPermission{RequestID: "request", CallbackID: "current", Resolved: make(chan struct{})}
	state := &interactiveState{agentSession: agent, pending: pending}
	e.interactiveStates["chat"] = state
	p := &stubPlatformEngine{n: "card"}
	msg := &Message{SessionKey: "chat", IsPermissionResponse: true}
	e.handlePendingPermission(p, msg, "perm:current:allow_all", "chat")
	if state.pending != pending || !pending.retryable || state.approveAll {
		t.Fatal("failed receipt cleared binding or enabled approve-all")
	}
	e.handlePendingPermission(p, msg, "perm:old:allow_all", "chat")
	if agent.calls != 1 {
		t.Fatal("stale retry reached the adapter")
	}
	e.handlePendingPermission(p, msg, "perm:current:allow_all", "chat")
	e.handlePendingPermission(p, msg, "perm:current:allow_all", "chat")
	if agent.calls != 2 || state.pending != nil || !state.approveAll {
		t.Fatal("valid retry did not resolve exactly once")
	}
}

func TestBoundPermissionConcurrentRepeatedClicksSubmitOnce(t *testing.T) {
	e := newTestEngine()
	agent := &recordingAgentSession{}
	e.interactiveStates["chat"] = &interactiveState{agentSession: agent, pending: &pendingPermission{RequestID: "request", CallbackID: "current", Resolved: make(chan struct{})}}
	p := &stubPlatformEngine{n: "card"}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			e.handlePendingPermission(p, &Message{SessionKey: "chat", IsPermissionResponse: true}, "perm:current:allow", "chat")
		}()
	}
	wg.Wait()
	if agent.calls != 1 {
		t.Fatalf("adapter received %d submissions, want one", agent.calls)
	}
}

func TestPermissionCallbackCannotAnswerAsyncUserInput(t *testing.T) {
	e := newTestEngine()
	question := &pendingUserInput{id: "question", questions: []UserQuestion{{Question: "Choose"}}, answers: make([]string, 1)}
	e.interactiveStates["chat"] = &interactiveState{pendingUserInput: question, agentSession: &stubAgentSession{}}
	p := &stubPlatformEngine{n: "card"}
	msg := &Message{SessionKey: "chat", IsPermissionResponse: true}
	for _, input := range []string{"perm:old:allow", "allow"} {
		content, handled := e.handleAsyncUserInput(p, msg, input, "chat", e.sessions)
		if handled || content != input || question.answers[0] != "" || question.responding || len(p.getSent()) != 0 {
			t.Fatal("permission callback was consumed as an async question answer")
		}
		if !e.handlePendingPermission(p, msg, content, "chat") {
			t.Fatal("stale callback escaped the permission path")
		}
	}
}
