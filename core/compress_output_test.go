package core

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"unicode/utf16"
	"unicode/utf8"
)

// Both actual delivery methods reject the Discord-sized limit, including
// non-BMP UTF-16 pairs. No network, real CLI or credentials are involved.
type compressLimitedPlatform struct {
	*stubPlatformEngine
	messages []string
	rejected bool
}

func (p *compressLimitedPlatform) Send(_ context.Context, _ any, text string) error {
	if len(utf16.Encode([]rune(text))) > 2000 || !utf8.ValidString(text) {
		p.rejected = true
		return fmt.Errorf("TEST_ONLY platform payload limit")
	}
	p.messages = append(p.messages, text)
	return nil
}

func (p *compressLimitedPlatform) Reply(ctx context.Context, replyCtx any, text string) error {
	return p.Send(ctx, replyCtx, text)
}

func TestProcessCompressEvents_TotalBudgetAndDelivery(t *testing.T) {
	for _, terminal := range []string{"empty_result", "final_result", "huge_result", "error", "eof", "invalid_result"} {
		t.Run(terminal, func(t *testing.T) {
			e := newTestEngine()
			e.display.ToolMaxLen = 0 // Exercise aggregate guard independent of per-tool preview.
			t.Cleanup(func() { if err := e.Stop(); err != nil { t.Errorf("Stop: %v", err) } })
			p := &compressLimitedPlatform{stubPlatformEngine: &stubPlatformEngine{n: "test"}}
			s := newQueuingSession("TEST_ONLY_compress_budget")
			state := &interactiveState{agentSession: s, platform: p}
			const key = "test:TEST_ONLY_compress_budget"
			session := e.sessions.GetOrCreateActive(key)
			lockGen, _ := session.TryLock()
			go func() {
				// Each output fits the per-event cap, but together exceed the
				// turn cap. Include huge raw/name events; invalid terminal UTF-8 has its own case.
				for i := 0; i < 100; i++ {
					s.events <- Event{Type: EventToolResult, ToolName: "TEST_ONLY_shell", Content: strings.Repeat("😀", 300)}
				}
				s.events <- Event{Type: EventText, Content: strings.Repeat("x", MaxToolOutputBytes+1)}
				s.events <- Event{Type: EventToolResult, ToolName: strings.Repeat("x", MaxToolOutputBytes+1), Content: "token=TEST_ONLY_FIXED_SECRET"}
				switch terminal {
				case "final_result": s.events <- Event{Type: EventResult, Content: "TEST_ONLY_FINAL_STATUS", Done: true}
				case "invalid_result": s.events <- Event{Type: EventResult, Content: strings.Repeat("\xffx", maxCompressOutputBytes/2), Done: true}
				case "huge_result": s.events <- Event{Type: EventResult, Content: strings.Repeat("x", MaxToolOutputBytes+1), Done: true}
				case "error": s.events <- Event{Type: EventError, Error: fmt.Errorf("%s", strings.Repeat("😀", 2000)+"TEST_ONLY_FINAL_ERROR")}
				case "eof": close(s.events)
				default: s.events <- Event{Type: EventResult, Done: true}
				}
			}()
			unlocked := false
			e.processCompressEvents(state, session, e.sessions, key, p, nil, &unlocked, false, lockGen)
			session.Unlock(lockGen)
			joined := strings.Join(p.messages, "")
			if p.rejected || joined == "" || strings.Contains(joined, "TEST_ONLY_FIXED_SECRET") {
				t.Fatal("unsafe or absent compression delivery")
			}
			if len(joined) > maxCompressOutputBytes+1024 { t.Fatalf("total output bytes = %d", len(joined)) }
			switch terminal {
			case "final_result":
				if joined != "TEST_ONLY_FINAL_STATUS" { t.Fatal("tool budget displaced final status") }
			case "huge_result", "invalid_result":
				if joined != "…" { t.Fatal("huge final output was not omitted") }
			case "error":
				if !strings.Contains(joined, "TEST_ONLY_FINAL_ERROR") || len(p.messages)<2 { t.Fatal("final error missing or not split") }
			default:
				if !strings.Contains(joined, "…") || len(p.messages)<2 { t.Fatal("aggregate not bounded and split") }
			}
		})
	}
}

func TestCompressOutputBudget_CountsPrefixesAndStopsGrowing(t *testing.T) {
	var b compressOutputBudget
	var out strings.Builder
	for i:=0; i<100000; i++ { out.WriteString(b.take("TEST_ONLY prefix: 😀\n")) }
	if out.Len()>maxCompressOutputBytes || b.used!=out.Len() || !b.omitted { t.Fatal("aggregate budget exceeded") }
	if got:=b.take("TEST_ONLY late"); got!="" { t.Fatal("exhausted budget grew") }
}

func TestBoundedCompressText_PreservesFragmentWhitespace(t *testing.T) {
	if got := boundedCompressText("one ") + boundedCompressText(" two\n"); got != "one  two\n" {
		t.Fatal("stream fragment whitespace changed")
	}
}

func TestCompressOutputBudget_InvalidUTF8Expansion(t *testing.T) {
	var b compressOutputBudget
	got := b.take(strings.Repeat("\xffx", maxCompressOutputBytes/2))
	if got != "\n…" || b.used > maxCompressOutputBytes || !utf8.ValidString(got) {
		t.Fatal("normalization escaped aggregate budget")
	}
	if got := boundedCompressText(strings.Repeat("\xffx", maxCompressOutputBytes/2)); got != "…" {
		t.Fatal("normalization escaped final budget")
	}
}

func TestCompressCommentary_PreservesOptInAndSilentPolicy(t *testing.T) {
	e := newTestEngine()
	t.Cleanup(func() { if err := e.Stop(); err != nil { t.Errorf("Stop: %v", err) } })
	p := &compressLimitedPlatform{stubPlatformEngine: &stubPlatformEngine{n: "test"}}
	var budget compressOutputBudget
	e.sendCompressCommentary(p, nil, "TEST_ONLY hidden", &budget)
	if len(p.messages) != 0 || budget.used != 0 { t.Fatal("commentary opt-in bypassed") }
	enabled := true
	e.display.CommentaryMessages = &enabled
	e.sendCompressCommentary(p, nil, "NO_REPLY", &budget)
	if len(p.messages) != 0 { t.Fatal("silent commentary was sent") }
	e.sendCompressCommentary(p, nil, strings.Repeat("😀", 2000), &budget)
	if p.rejected || len(p.messages) != 2 || budget.used != 8000 { t.Fatal("commentary not budgeted/split") }
}

func TestProcessCompressEvents_HonorsToolPreviewLimit(t *testing.T) {
	e := newTestEngine()
	e.display.ToolMaxLen = 32
	t.Cleanup(func() { if err := e.Stop(); err != nil { t.Errorf("Stop: %v", err) } })
	p := &compressLimitedPlatform{stubPlatformEngine: &stubPlatformEngine{n: "test"}}
	s := newQueuingSession("TEST_ONLY_preview_limit")
	state := &interactiveState{agentSession: s, platform: p}
	const key = "test:TEST_ONLY_preview_limit"
	session := e.sessions.GetOrCreateActive(key)
	lockGen, _ := session.TryLock()
	s.events <- Event{Type: EventToolResult, ToolName: "TEST_ONLY_shell", Content: strings.Repeat("x", 3000)}
	s.events <- Event{Type: EventResult, Done: true}
	unlocked := false
	e.processCompressEvents(state, session, e.sessions, key, p, nil, &unlocked, false, lockGen)
	session.Unlock(lockGen)
	joined := strings.Join(p.messages, "")
	if p.rejected || len(joined)>200 || !strings.Contains(joined, "…") { t.Fatal("configured tool preview limit ignored") }
}
