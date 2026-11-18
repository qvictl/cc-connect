package core

import (
	"strings"
	"testing"
	"time"
)

// The test closes the event stream itself; cleanup must not close it twice.
type narrationReviewSession struct {
	*controllableAgentSession
}

func (s *narrationReviewSession) Close() error { return nil }

func TestNarrationAbortDoesNotPromoteProgress(t *testing.T) {
	for _, test := range []struct {
		name       string
		cancelled  bool
		stopped    bool
		close      bool
		result     string
		tail       string
		wantAnswer string
	}{
		{name: "cancelled_result", cancelled: true},
		{name: "cancelled_result_preserves_answer_tail", cancelled: true, tail: "Answer already streamed.", wantAnswer: "Answer already streamed."},
		{name: "cancelled_result_ignores_whitespace_tail", cancelled: true, tail: "  \t"},
		{name: "cancelled_result_silent_result_overrides_tail", cancelled: true, result: "NO_REPLY", tail: "Answer already streamed."},
		{name: "cancelled_channel_close_preserves_answer_tail", cancelled: true, close: true, tail: "Answer already streamed.", wantAnswer: "Answer already streamed."},
		{name: "cancelled_channel_close_ignores_whitespace_tail", cancelled: true, close: true, tail: "  \t"},
		{name: "cancelled_channel_close", cancelled: true, close: true},
		{name: "stopped_channel_close", stopped: true, close: true},
		{name: "ordinary_channel_close", close: true, wantAnswer: "Checking."},
		{name: "explicit_result_remains_authoritative", cancelled: true, result: "Completed.", wantAnswer: "Completed."},
	} {
		t.Run(test.name, func(t *testing.T) {
			p := newProgressCardPlatform()
			e := newNarrationEngine(t, p, narrationProgress)
			key := "test:narration-abort"
			session := e.sessions.GetOrCreateActive(key)
			s := &narrationReviewSession{newControllableSession("narration")}
			state := &interactiveState{agentSession: s, platform: p, replyCtx: "ctx"}
			e.interactiveMu.Lock()
			e.interactiveStates[key] = state
			e.interactiveMu.Unlock()
			done := make(chan struct{})
			go func() {
				e.processInteractiveEvents(state, session, e.sessions, key, "m", time.Now(), nil, nil, "ctx", 0)
				close(done)
			}()
			s.events <- Event{Type: EventText, Content: "Checking."}
			s.events <- Event{Type: EventToolUse, ToolName: "Read"}
			deadline := time.Now().Add(2 * time.Second)
			for len(p.getPreviewStarts()) == 0 && time.Now().Before(deadline) {
				time.Sleep(time.Millisecond)
			}
			if len(p.getPreviewStarts()) == 0 {
				t.Fatal("narration was not drawn before abort")
			}
			if test.tail != "" {
				s.events <- Event{Type: EventText, Content: test.tail}
			}
			state.mu.Lock()
			state.turnCancelled = test.cancelled
			state.stopped = test.stopped
			state.mu.Unlock()
			if test.close {
				close(s.events)
			} else {
				s.events <- Event{Type: EventResult, Content: test.result, Done: true}
			}
			select {
			case <-done:
			case <-time.After(3 * time.Second):
				t.Fatal("turn did not complete")
			}
			sent := p.getSent()
			if test.wantAnswer == "" {
				if len(sent) != 0 {
					t.Fatalf("aborted turn promoted narration: %q", sent)
				}
			} else if len(sent) != 1 || sent[0] != test.wantAnswer {
				t.Fatalf("sent = %q, want %q", sent, test.wantAnswer)
			}
		})
	}
}

// The existing candidate gives narration precedence over rich cards and flushes
// at boundaries even when quiet mode hides tool/thinking messages.
func TestNarrationQuietAndRichKeepSeparateAnswer(t *testing.T) {
	for _, mode := range []string{"full", "quiet"} {
		for _, cardMode := range []string{"legacy", "rich"} {
			t.Run(mode+"_"+cardMode, func(t *testing.T) {
				p := newProgressCardPlatform() // also implements RichCardSupporter
				e := newNarrationEngine(t, p, narrationProgress)
				e.SetDisplayConfig(DisplayCfg{Mode: mode, CardMode: cardMode, Narration: narrationProgress})
				runNarrationTurn(t, e, p, "test:narration-modes",
					Event{Type: EventText, Content: "Let me check the board."},
					Event{Type: EventToolUse, ToolName: "Read"},
					Event{Type: EventText, Content: "Two tasks are open."},
					Event{Type: EventResult, Done: true},
				)
				if sent := p.getSent(); len(sent) != 1 || sent[0] != "Two tasks are open." {
					t.Fatalf("sent = %q", sent)
				}
				if got := strings.Join(infoTexts(lastProgressItems(t, p)), "|"); got != "Let me check the board." {
					t.Fatalf("narration = %q", got)
				}
			})
		}
	}
}

func TestNarrationHoldsSilentPrefixAcrossBoundary(t *testing.T) {
	for _, boundary := range []EventType{EventToolUse, EventThinking} {
		for _, tail := range []string{"_REPLY", " ordinary answer"} {
			t.Run(string(boundary)+tail, func(t *testing.T) {
				p := newProgressCardPlatform()
				e := newNarrationEngine(t, p, narrationProgress)
				runNarrationTurn(t, e, p, "test:narration-prefix",
					Event{Type: EventText, Content: "NO"},
					Event{Type: boundary, ToolName: "Read", Content: "thinking"},
					Event{Type: EventText, Content: tail},
					Event{Type: EventResult, Done: true},
				)
				for _, drawn := range append(p.getPreviewStarts(), p.getPreviewEdits()...) {
					payload, ok := ParseProgressCardPayload(drawn)
					if !ok {
						t.Fatalf("unexpected non-progress preview: %q", drawn)
					}
					if infos := infoTexts(payload.Items); len(infos) != 0 {
						t.Fatalf("silent prefix leaked into progress: %q", infos)
					}
				}
				sent := p.getSent()
				if tail == "_REPLY" {
					if len(sent) != 0 {
						t.Fatalf("silent reply leaked: %q", sent)
					}
				} else if len(sent) != 1 || sent[0] != "NO ordinary answer" {
					t.Fatalf("ordinary prefix text lost: %q", sent)
				}
			})
		}
	}
}
