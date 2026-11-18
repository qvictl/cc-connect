package dsh

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/chenhg5/cc-connect/core"
)

// These fixtures exercise the event mapper directly: no client, server,
// process, credentials, timers, or network are needed.
func newToolDeltaTestSession(t *testing.T) *dshSession {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	s := &dshSession{
		ctx:              ctx,
		events:           make(chan core.Event, 64),
		toolNames:        make(map[string]string),
		seenCalls:        make(map[string]struct{}),
		pendingApprovals: make(map[string]pendingApproval),
		pendingQuestions: make(map[string]pendingQuestion),
	}
	s.textAgg = newChunkAggregator(1024, 0, func(text string) {
		s.emit(core.Event{Type: core.EventText, Content: text})
	})
	s.thinkAgg = newChunkAggregator(1024, 0, func(text string) {
		s.emit(core.Event{Type: core.EventThinking, Content: text})
	})
	return s
}

func feedToolDeltaSessionEvent(t *testing.T, s *dshSession, kind string, data sessionEventData) {
	t.Helper()
	body, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(sessionEvent{Type: kind, Data: body})
	if err != nil {
		t.Fatal(err)
	}
	s.handleSessionEvent(raw)
}

func collectToolDeltaEvents(s *dshSession) []core.Event {
	var events []core.Event
	for {
		select {
		case event := <-s.events:
			events = append(events, event)
		default:
			return events
		}
	}
}

// Regression for #2087: a streamed fragment must never claim the complete
// call's slot. Replaying either source must not append duplicate card steps.
func TestSession_ToolCallDeltaUsesDurableArgumentsRegardlessOfOrder(t *testing.T) {
	const full = `{"command":"pwd","directory":"/workspace/TEST_ONLY_PROJECT"}`
	for _, tc := range []struct {
		name  string
		order []string
	}{
		{"delta first", []string{"delta", "continuation", "call"}},
		{"durable first", []string{"call", "delta", "continuation"}},
		{"replayed delta and durable", []string{"delta", "delta", "call", "continuation", "call"}},
		{"nameless delta first", []string{"continuation", "call"}},
		{"durable only", []string{"call", "call"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newToolDeltaTestSession(t)
			for _, step := range tc.order {
				switch step {
				case "delta":
					s.handleChunk(&streamChunk{Type: "tool-call-delta", ID: "TEST_ONLY_CALL", Name: "bash", ArgsDelt: `{"command":`})
				case "continuation":
					s.handleChunk(&streamChunk{Type: "tool-call-delta", ID: "TEST_ONLY_CALL", ArgsDelt: `"pwd"}`})
				case "call":
					feedToolDeltaSessionEvent(t, s, "tool/call", sessionEventData{CallID: "TEST_ONLY_CALL", Name: "bash", Args: full})
				}
			}
			events := collectToolDeltaEvents(s)
			if len(events) != 1 || events[0].Type != core.EventToolUse {
				t.Fatalf("expected one durable tool step, got %+v", events)
			}
			if events[0].ToolCallID != "TEST_ONLY_CALL" || events[0].ToolName != "bash" || events[0].ToolInput != full || !json.Valid([]byte(events[0].ToolInput)) {
				t.Fatalf("tool step did not use complete durable arguments: %+v", events[0])
			}
		})
	}
}

func TestSession_ToolCallDeltaFlushesProgressWithoutPartialStep(t *testing.T) {
	for _, name := range []string{"bash", ""} {
		t.Run("name="+name, func(t *testing.T) {
			s := newToolDeltaTestSession(t)
			s.handleChunk(&streamChunk{Type: "text-delta", Text: "preparing tool"})
			s.handleChunk(&streamChunk{Type: "reasoning-delta", Text: "checking arguments"})
			if len(s.events) != 0 {
				t.Fatal("fixture unexpectedly flushed before tool boundary")
			}
			s.handleChunk(&streamChunk{Type: "tool-call-delta", ID: "TEST_ONLY_CALL", Name: name, ArgsDelt: `{"command":`})
			events := collectToolDeltaEvents(s)
			if len(events) != 2 || events[0].Type != core.EventText || events[0].Content != "preparing tool" ||
				events[1].Type != core.EventThinking || events[1].Content != "checking arguments" {
				t.Fatalf("delta did not flush only preceding progress: %+v", events)
			}
			if _, claimed := s.seenCalls["TEST_ONLY_CALL"]; claimed {
				t.Fatal("provisional delta claimed the durable call id")
			}
		})
	}
}

func TestSession_ToolCallDeltaPreservesDurableResultName(t *testing.T) {
	s := newToolDeltaTestSession(t)
	s.handleChunk(&streamChunk{Type: "tool-call-delta", ID: "TEST_ONLY_CALL", Name: "provisional", ArgsDelt: `{`})
	if s.toolNames["TEST_ONLY_CALL"] != "provisional" {
		t.Fatal("delta lost the fallback name for a result without a durable call")
	}
	feedToolDeltaSessionEvent(t, s, "tool/call", sessionEventData{CallID: "TEST_ONLY_CALL", Name: "bash", Args: `{"command":"pwd"}`})
	s.handleChunk(&streamChunk{Type: "tool-call-delta", ID: "TEST_ONLY_CALL", Name: "stale provisional", ArgsDelt: `}`})
	feedToolDeltaSessionEvent(t, s, "tool/call", sessionEventData{CallID: "TEST_ONLY_CALL", Name: "stale replay", Args: `{}`})
	feedToolDeltaSessionEvent(t, s, "tool/result", sessionEventData{CallID: "TEST_ONLY_CALL"})
	events := collectToolDeltaEvents(s)
	if len(events) != 2 || events[0].Type != core.EventToolUse || events[1].Type != core.EventToolResult ||
		events[1].ToolCallID != "TEST_ONLY_CALL" || events[1].ToolName != "bash" {
		t.Fatalf("late delta or replay changed durable result identity: %+v", events)
	}
}

func TestSession_ToolCallDeltaKeepsInterleavedCallsIndependent(t *testing.T) {
	s := newToolDeltaTestSession(t)
	s.handleChunk(&streamChunk{Type: "tool-call-delta", ID: "TEST_ONLY_A", Name: "bash", ArgsDelt: `{`})
	s.handleChunk(&streamChunk{Type: "tool-call-delta", ID: "TEST_ONLY_B", Name: "bash", ArgsDelt: `{`})
	feedToolDeltaSessionEvent(t, s, "tool/call", sessionEventData{CallID: "TEST_ONLY_B", Name: "bash", Args: `{"command":"ls"}`})
	feedToolDeltaSessionEvent(t, s, "tool/call", sessionEventData{CallID: "TEST_ONLY_A", Name: "bash", Args: `{"command":"pwd"}`})
	// Same-name results arrive in reverse call order; native ids must pair
	// them rather than relying on the core's legacy name/order fallback.
	feedToolDeltaSessionEvent(t, s, "tool/result", sessionEventData{CallID: "TEST_ONLY_A"})
	feedToolDeltaSessionEvent(t, s, "tool/result", sessionEventData{CallID: "TEST_ONLY_B"})
	events := collectToolDeltaEvents(s)
	if len(events) != 4 || events[0].Type != core.EventToolUse || events[0].ToolCallID != "TEST_ONLY_B" || events[0].ToolInput != `{"command":"ls"}` ||
		events[1].Type != core.EventToolUse || events[1].ToolCallID != "TEST_ONLY_A" || events[1].ToolInput != `{"command":"pwd"}` ||
		events[2].Type != core.EventToolResult || events[2].ToolCallID != "TEST_ONLY_A" || events[2].ToolName != "bash" ||
		events[3].Type != core.EventToolResult || events[3].ToolCallID != "TEST_ONLY_B" || events[3].ToolName != "bash" {
		t.Fatalf("interleaved calls did not keep durable arguments: %+v", events)
	}
	feedToolDeltaSessionEvent(t, s, "turn/start", sessionEventData{Turn: 2})
	feedToolDeltaSessionEvent(t, s, "tool/call", sessionEventData{CallID: "TEST_ONLY_A", Name: "bash", Args: `{"command":"ls"}`})
	events = collectToolDeltaEvents(s)
	if len(events) != 1 || events[0].Type != core.EventToolUse || events[0].ToolInput != `{"command":"ls"}` {
		t.Fatalf("previous turn suppressed a fresh call: %+v", events)
	}
}

// Questions and approvals have their own frames and ownership maps. Waiting
// for a durable tool input must neither suppress nor resolve those requests.
func TestSession_ToolCallDeltaDoesNotConsumeQuestionOrApproval(t *testing.T) {
	s := newToolDeltaTestSession(t)
	s.handleChunk(&streamChunk{Type: "tool-call-delta", ID: "TEST_ONLY_CALL", Name: "bash", ArgsDelt: `{`})
	s.handleFrame(muxFrame{
		Type: "question/requested", RPCID: "TEST_ONLY_QUESTION", SessionID: "TEST_ONLY_SESSION",
		Questions: []wireQuestion{{Question: "Which branch?"}},
	})
	s.handleFrame(muxFrame{
		Type: "approval/requested", RPCID: "TEST_ONLY_APPROVAL", SessionID: "TEST_ONLY_SESSION",
		CallID: "TEST_ONLY_CALL", ToolName: "bash", Reason: "Complete approval reason",
	})
	feedToolDeltaSessionEvent(t, s, "tool/call", sessionEventData{CallID: "TEST_ONLY_CALL", Name: "bash", Args: `{"command":"pwd"}`})
	events := collectToolDeltaEvents(s)
	if len(events) != 3 {
		t.Fatalf("unexpected tool/interaction events: %+v", events)
	}
	question, approval, call := events[0], events[1], events[2]
	if question.Type != core.EventPermissionRequest || question.ToolName != "AskUserQuestion" ||
		question.RequestID != "TEST_ONLY_QUESTION" || len(question.Questions) != 1 || question.Questions[0].Question != "Which branch?" {
		t.Fatalf("question frame was changed or hidden: %+v", question)
	}
	if approval.Type != core.EventPermissionRequest || approval.RequestID != "TEST_ONLY_APPROVAL" || approval.ToolInput != "Complete approval reason" {
		t.Fatalf("approval frame was changed or hidden: %+v", approval)
	}
	if call.Type != core.EventToolUse || call.ToolInput != `{"command":"pwd"}` {
		t.Fatalf("durable call was changed or hidden: %+v", call)
	}
	if len(s.pendingQuestions) != 1 || len(s.pendingApprovals) != 1 {
		t.Fatal("tool input processing consumed a pending interaction")
	}
}
