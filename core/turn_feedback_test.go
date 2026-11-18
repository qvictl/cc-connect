package core

import (
	"context"
	"fmt"
	"testing"
)

// stubFeedbackPlatform is a footer-capable platform that ends final replies
// with feedback controls.
type stubFeedbackPlatform struct {
	stubFooterSendingPlatform
	feedback bool
}

func (p *stubFeedbackPlatform) TurnFeedbackEnabled() bool { return p.feedback }

// With feedback controls on, the final reply takes the footer path even
// when there is no footer text: that is where the controls are rendered.
func TestSendChunksWithStatusFooter_FeedbackControlsUseFooterPathWithoutFooter(t *testing.T) {
	send := func(pl Platform, rctx any, content string) error {
		return pl.Send(context.Background(), rctx, content)
	}
	on := &stubFeedbackPlatform{stubFooterSendingPlatform: stubFooterSendingPlatform{stubPlatformEngine: stubPlatformEngine{n: "test"}}, feedback: true}
	if !sendChunksWithStatusFooter(context.Background(), on, "ctx", "hello body", "", send) {
		t.Fatal("expected success")
	}
	if len(on.footerCalls) != 1 || on.footerCalls[0] != "hello body|FOOTER|" {
		t.Fatalf("footer calls = %#v, want the body with an empty footer", on.footerCalls)
	}
	if got := on.getSent(); len(got) != 0 {
		t.Fatalf("plain Send = %#v, want none", got)
	}

	off := &stubFeedbackPlatform{stubFooterSendingPlatform: stubFooterSendingPlatform{stubPlatformEngine: stubPlatformEngine{n: "test"}}}
	if !sendChunksWithStatusFooter(context.Background(), off, "ctx", "hello body", "", send) {
		t.Fatal("expected success")
	}
	if len(off.footerCalls) != 0 {
		t.Fatalf("controls off and no footer must send plainly, got footer calls %#v", off.footerCalls)
	}
	if got := off.getSent(); len(got) != 1 || got[0] != "hello body" {
		t.Fatalf("plain Send = %#v", got)
	}
}

type stubAttachingPlatform struct {
	stubFeedbackPlatform
	attached int
}

func (p *stubAttachingPlatform) AttachTurnFeedback(context.Context, any) error {
	p.attached++
	return nil
}

// A turn whose answer went out before its last tool call has no final reply
// to carry the controls; the platform is asked to attach them, only when on.
func TestAttachTurnFeedback_OnlyWhenTheControlsAreOn(t *testing.T) {
	on := &stubAttachingPlatform{stubFeedbackPlatform: stubFeedbackPlatform{feedback: true}}
	attachTurnFeedback(context.Background(), on, "ctx")
	if on.attached != 1 {
		t.Fatalf("attached %d times, want 1", on.attached)
	}
	off := &stubAttachingPlatform{}
	attachTurnFeedback(context.Background(), off, "ctx")
	if off.attached != 0 {
		t.Fatalf("controls off must not attach, attached %d", off.attached)
	}
	attachTurnFeedback(context.Background(), &stubPlatformEngine{n: "plain"}, "ctx")
}

// feedbackDeliveryPlatform models a stale platform target surviving until the
// engine explicitly begins a new turn and captures a successfully sent answer.
type feedbackDeliveryPlatform struct {
	stubAttachingPlatform
	begun []any
	captured []any
	attachedCtx []any
	failSend bool
}

func (p *feedbackDeliveryPlatform) BeginTurnFeedback(ctx any) {
	p.begun = append(p.begun, ctx)
	p.captured = nil
}

func (p *feedbackDeliveryPlatform) CaptureTurnFeedbackAnswer(ctx any) {
	p.captured = append(p.captured, ctx)
}

func (p *feedbackDeliveryPlatform) AttachTurnFeedback(_ context.Context, ctx any) error {
	p.attached++
	p.attachedCtx = append(p.attachedCtx, ctx)
	return nil
}

func (p *feedbackDeliveryPlatform) Send(ctx context.Context, rctx any, content string) error {
	if p.failSend {
		return fmt.Errorf("simulated answer send failure")
	}
	return p.stubPlatformEngine.Send(ctx, rctx, content)
}

func newFeedbackDeliveryPlatform() *feedbackDeliveryPlatform {
	return &feedbackDeliveryPlatform{stubAttachingPlatform: stubAttachingPlatform{
		stubFeedbackPlatform: stubFeedbackPlatform{
			stubFooterSendingPlatform: stubFooterSendingPlatform{stubPlatformEngine: stubPlatformEngine{n: "test"}},
			feedback: true,
		},
	}}
}

func TestTurnFeedback_FallbackCapturesOriginalReplyContext(t *testing.T) {
	p := newFeedbackDeliveryPlatform()
	p.failFooter = true
	ctx := &struct{ ID string }{"original reply"}
	send := func(pl Platform, rctx any, content string) error {
		return pl.Send(context.Background(), rctx, content)
	}
	if !sendChunksWithStatusFooter(context.Background(), p, ctx, "answer", "", send) {
		t.Fatal("ordinary fallback should deliver the answer")
	}
	if len(p.captured) != 1 || p.captured[0] != ctx || len(p.attachedCtx) != 1 || p.attachedCtx[0] != ctx {
		t.Fatalf("capture/attachment lost original context: %#v / %#v", p.captured, p.attachedCtx)
	}
}

func TestTurnFeedback_FailedFallbackDoesNotCaptureOrAttach(t *testing.T) {
	p := newFeedbackDeliveryPlatform()
	p.failFooter, p.failSend = true, true
	send := func(pl Platform, rctx any, content string) error {
		return pl.Send(context.Background(), rctx, content)
	}
	if sendChunksWithStatusFooter(context.Background(), p, "ctx", "answer", "", send) {
		t.Fatal("failed fallback reported success")
	}
	if len(p.captured) != 0 || p.attached != 0 {
		t.Fatal("failed answer must not capture or attach")
	}
}

func TestTurnFeedback_SilentAfterToolRequiresDeliveredAnswer(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(fmt.Sprint(fail), func(t *testing.T) {
			p := newFeedbackDeliveryPlatform()
			p.failSend = fail
			e := NewEngine("test", &stubAgent{}, []Platform{p}, "", LangEnglish)
			t.Cleanup(func() { _ = e.Stop() })
			e.SetDisplayConfig(DisplayCfg{Mode: "full", ToolMessages: true})
			e.SetStreamPreviewCfg(StreamPreviewCfg{Enabled: false})
			e.SetReplyFooterEnabled(false)
			e.SetShowContextIndicator(false)
			runNarrationTurn(t, e, p, "test:feedback-silent",
				Event{Type: EventText, Content: "delivered answer"},
				Event{Type: EventToolUse, ToolName: "shell"},
				Event{Type: EventResult, Content: "NO_REPLY", Done: true})
			want := 1
			if fail {
				want = 0
			}
			if len(p.begun) != 1 || len(p.captured) != want || p.attached != want {
				t.Fatalf("begun=%d captured=%d attached=%d; want 1,%d,%d", len(p.begun), len(p.captured), p.attached, want, want)
			}
		})
	}
}

func TestTurnFeedback_HookRejectionClearsDeliveredAnswer(t *testing.T) {
	p := newFeedbackDeliveryPlatform()
	e := NewEngine("test", &stubAgent{}, []Platform{p}, "", LangEnglish)
	t.Cleanup(func() { _ = e.Stop() })
	e.SetDisplayConfig(DisplayCfg{Mode: "full", ToolMessages: true})
	e.SetStreamPreviewCfg(StreamPreviewCfg{Enabled: false})
	runNarrationTurn(t, e, p, "test:feedback-rejection",
		Event{Type: EventText, Content: "rejected answer"},
		Event{Type: EventToolUse, ToolName: "shell"},
		Event{Type: EventHookRejected},
		Event{Type: EventResult, Content: "NO_REPLY", Done: true})
	if len(p.begun) != 2 || len(p.captured) != 0 || p.attached != 0 {
		t.Fatalf("rejected answer survived: begun=%d captured=%d attached=%d", len(p.begun), len(p.captured), p.attached)
	}
}

type feedbackFailingPreview struct {
	mockUpdaterPlatform
	feedback bool
	failUpdate bool
}

func (p *feedbackFailingPreview) TurnFeedbackEnabled() bool { return p.feedback }
func (p *feedbackFailingPreview) UpdateMessageWithStatusFooter(context.Context, any, string, string) error {
	return fmt.Errorf("simulated structured update failure")
}
func (p *feedbackFailingPreview) UpdateMessage(ctx context.Context, h any, text string) error {
	if p.failUpdate {
		return fmt.Errorf("simulated preview update failure")
	}
	return p.mockUpdaterPlatform.UpdateMessage(ctx, h, text)
}

func TestTurnFeedback_FailedStructuredPreviewRequiresFreshFinal(t *testing.T) {
	for _, degraded := range []bool{false, true} {
		p := &feedbackFailingPreview{feedback: true}
		sp := newStreamPreview(DefaultStreamPreviewCfg(), p, "reply-context", context.Background(), nil)
		sp.previewMsgID, sp.fullText, sp.degraded = "preview", "answer", degraded
		if sp.finish("answer", "") {
			t.Fatalf("degraded=%v: ordinary update cannot substitute for controls", degraded)
		}
		if len(p.getMessages()) != 0 {
			t.Fatal("plain update must not masquerade as completed feedback finalization")
		}
	}
}

func TestTurnFeedback_FreezeReportsOnlySuccessfulAnswerUpdate(t *testing.T) {
	for _, tc := range []struct { text string; fail bool; want bool }{
		{"answer", false, true}, {"answer", true, false}, {"NO_REPLY", false, false}, {"", false, false},
	} {
		p := &feedbackFailingPreview{failUpdate: tc.fail}
		sp := newStreamPreview(DefaultStreamPreviewCfg(), p, "ctx", context.Background(), nil)
		sp.previewMsgID, sp.fullText = "preview", tc.text
		if got := sp.freeze(); got != tc.want {
			t.Fatalf("text=%q fail=%v delivered=%v want=%v", tc.text, tc.fail, got, tc.want)
		}
	}
}

type feedbackNarrationPlatform struct {
	*stubCompactProgressPlatform
	failPreview bool
	captured int
	attached int
}

func (p *feedbackNarrationPlatform) TurnFeedbackEnabled() bool { return true }
func (p *feedbackNarrationPlatform) CaptureTurnFeedbackAnswer(any) { p.captured++ }
func (p *feedbackNarrationPlatform) AttachTurnFeedback(context.Context, any) error {
	p.attached++
	return nil
}
func (p *feedbackNarrationPlatform) SendPreviewStart(ctx context.Context, rctx any, text string) (any, error) {
	if p.failPreview {
		return nil, fmt.Errorf("simulated progress failure")
	}
	return p.stubCompactProgressPlatform.SendPreviewStart(ctx, rctx, text)
}

func TestTurnFeedback_NarrationSilentProgressNeverAttaches(t *testing.T) {
	p := &feedbackNarrationPlatform{stubCompactProgressPlatform: newProgressCardPlatform()}
	e := newNarrationEngine(t, p, narrationProgress)
	runNarrationTurn(t, e, p, "test:feedback-narration-silent",
		Event{Type: EventText, Content: "NO_REPLY"},
		Event{Type: EventToolUse, ToolName: "shell"},
		Event{Type: EventResult, Content: "NO_REPLY", Done: true})
	if p.captured != 0 || p.attached != 0 {
		t.Fatalf("silent progress received feedback: capture=%d attach=%d", p.captured, p.attached)
	}
}

func TestTurnFeedback_NarrationDuplicateCapturesDeliveredFallback(t *testing.T) {
	p := &feedbackNarrationPlatform{stubCompactProgressPlatform: newProgressCardPlatform(), failPreview: true}
	e := newNarrationEngine(t, p, narrationProgress)
	runNarrationTurn(t, e, p, "test:feedback-narration-fallback",
		Event{Type: EventText, Content: "the answer"},
		Event{Type: EventToolUse, ToolName: "shell"},
		Event{Type: EventResult, Content: "the answer", Done: true})
	if p.captured != 1 || p.attached != 1 {
		t.Fatalf("delivered narration lost feedback: capture=%d attach=%d", p.captured, p.attached)
	}
}
