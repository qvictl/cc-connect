package core

import (
	"strings"
	"testing"
	"time"
)

func TestUnsupportedQuestionClassificationPreservesSupportedShapes(t *testing.T) {
	for _, tool := range []string{"AskUserQuestion", "extension_select"} {
		event := Event{Type: EventPermissionRequest, ToolName: tool, Questions: []UserQuestion{{Question: "Choose"}}}
		if !isAskQuestionRequest(event) || isUnsupportedQuestionRequest(event) {
			t.Fatal("supported question shape changed")
		}
	}
	if isUnsupportedQuestionRequest(Event{Type: EventPermissionRequest, ToolName: "extension_confirm"}) {
		t.Fatal("ordinary confirmation became an unsupported question")
	}
	if !isUnsupportedQuestionRequest(Event{Type: EventPermissionRequest, Questions: []UserQuestion{{Question: "Choose"}}}) {
		t.Fatal("unrecognized question shape was treated as an approval")
	}
}

func TestForegroundUnsupportedQuestionNeverApprovesOrRendersControls(t *testing.T) {
	for _, approveAll := range []bool{false, true} {
		p := &stubInlineButtonPlatform{stubPlatformEngine: stubPlatformEngine{n: "inline"}}
		e := NewEngine("test", &stubAgent{}, []Platform{p}, "", LangEnglish)
		s := &questionSession{controllableAgentSession: controllableAgentSession{
			sessionID: "unsupported", alive: true, events: make(chan Event, 3), closed: make(chan struct{}),
		}}
		key := "test:unsupported"
		state := &interactiveState{agentSession: s, platform: p, replyCtx: "ctx", approveAll: approveAll}
		s.events <- Event{Type: EventPermissionRequest, RequestID: "question", Questions: []UserQuestion{{Question: "Which project?"}}}
		s.events <- Event{Type: EventResult, Content: "done", Done: true}
		e.processInteractiveEvents(state, e.sessions.GetOrCreateActive(key), e.sessions, key, "m", time.Now(), nil, nil, "ctx", 0)
		if len(s.recorded()) != 0 || state.pending != nil || len(p.buttonRows) != 0 {
			t.Fatal("unsupported question created pending controls or submitted a boolean approval")
		}
		found := false
		for _, text := range p.getSent() {
			if strings.Contains(text, "Which project?") && strings.Contains(text, "agent's own interface") && strings.Contains(text, "No answer or approval was submitted") {
				found = true
			}
		}
		if !found {
			t.Fatal("missing unsupported-question guidance")
		}
		_ = e.Stop()
	}
}

func TestUnsolicitedUnsupportedQuestionNeverAutoApproves(t *testing.T) {
	for _, approveAll := range []bool{false, true} {
		_, p, s, state := startUnsolicitedQuestionTest(t, approveAll)
		s.events <- Event{Type: EventPermissionRequest, RequestID: "question", Questions: []UserQuestion{{Question: "Which project?"}}}
		deadline := time.After(time.Second)
		for !strings.Contains(strings.Join(p.getSent(), "\n"), "agent's own interface") {
			select {
			case <-deadline:
				t.Fatal("unsupported unsolicited question has no feedback")
			case <-time.After(time.Millisecond):
			}
		}
		state.mu.Lock()
		pending := state.pending
		state.mu.Unlock()
		if pending != nil || len(s.recorded()) != 0 {
			t.Fatal("unsupported unsolicited question was answered or made pending")
		}
	}
}
