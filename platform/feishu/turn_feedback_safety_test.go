package feishu

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

// These regressions use in-memory seams and the fixed, target-locked HTTP
// fixture. They never need a real command, inherited credentials or a shell.
func TestFeedbackOutput_BoundedAndDrains(t *testing.T) {
	b := feedbackOutput{limit: 8}
	for _, input := range []string{"1234", "5678", strings.Repeat("TEST_ONLY_OUTPUT", 1000)} {
		n, err := b.Write([]byte(input))
		if n != len(input) || err != nil {
			t.Fatal("bounded output did not consume the complete write")
		}
	}
	if string(b.data) != "12345678" || !b.truncated {
		t.Fatal("output was not bounded at the exact byte limit")
	}
	exact := feedbackOutput{limit: 4}
	_, _ = exact.Write([]byte("1234"))
	if exact.truncated {
		t.Fatal("exact limit must not be considered truncated")
	}
}

func TestFeedbackCommandEnv_ExplicitAllowlist(t *testing.T) {
	got := feedbackCommandEnv("TEST_ONLY_SESSION", "/TEST_ONLY_HOME", "/TEST_ONLY_BIN")
	want := []string{"PATH=/TEST_ONLY_BIN", "HOME=/TEST_ONLY_HOME", "LANG=C.UTF-8", "CC_SESSION_KEY=TEST_ONLY_SESSION"}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatal("command environment includes unexpected inherited settings")
	}
}

func TestTurnFeedback_RejectsUnknownOrMismatchedDestination(t *testing.T) {
	ip, calls := newTurnFeedbackPlatform(t)
	for _, tc := range []struct {
		name, session, chat, message string
	}{
		{"unknown", "feishu:oc_TEST_ONLY_CHAT:root:om_TEST_ONLY_ROOT", "oc_TEST_ONLY_CHAT", "om_TEST_ONLY_UNKNOWN"},
		{"other chat", "feishu:oc_TEST_ONLY_CHAT:root:om_TEST_ONLY_ROOT", "oc_TEST_ONLY_OTHER", "om_TEST_ONLY_REPLY"},
		{"other thread", "feishu:oc_TEST_ONLY_CHAT:root:om_TEST_ONLY_OTHER", "oc_TEST_ONLY_CHAT", "om_TEST_ONLY_REPLY"},
		{"missing chat", "feishu:oc_TEST_ONLY_CHAT:root:om_TEST_ONLY_ROOT", "", "om_TEST_ONLY_REPLY"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp := ip.onTurnFeedback("up", nil, tc.session, &callback.Context{OpenChatID: tc.chat, OpenMessageID: tc.message}, "ou_TEST_ONLY_CLICKER")
			if resp == nil || resp.Toast == nil || resp.Toast.Type != "error" || resp.Card != nil {
				t.Fatal("unbound feedback was not rejected")
			}
		})
	}
	select {
	case <-calls:
		t.Fatal("unbound callback reached the command seam")
	default:
	}
}

func TestTurnFeedback_QueueFullDoesNotClaimOrPersistSuccess(t *testing.T) {
	ip, _ := newTurnFeedbackPlatform(t)
	started := make(chan struct{})
	finished := make(chan struct{})
	ip.turnFeedbackRun = func(ctx context.Context, _ string, _ []string) {
		close(started)
		<-ctx.Done()
		close(finished)
	}
	if resp, _ := feedbackClick(ip, "up", nil); resp.Toast.Type != "success" {
		t.Fatal("first click was not accepted")
	}
	<-started
	for i := 0; i < 256; i++ {
		if !ip.enqueueTurnFeedback(func(context.Context, string, []string) {}, "TEST_ONLY_SESSION", nil) {
			t.Fatal("queue filled before its documented capacity")
		}
	}
	resp, _ := feedbackClick(ip, "down", map[string]any{"comment": "TEST_ONLY_REJECTED_COMMENT"})
	if resp == nil || resp.Toast.Type != "error" || resp.Card != nil {
		t.Fatal("full queue falsely acknowledged or redrew the click")
	}
	card, _ := ip.feedbackCards.lookup("om_TEST_ONLY_REPLY")
	if card.Verdict != "up" || card.Comment != "" {
		t.Fatal("rejected click changed persistent feedback")
	}
	if err := ip.Stop(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("Stop did not cancel the active feedback job")
	}
	if ip.enqueueTurnFeedback(func(context.Context, string, []string) {}, "TEST_ONLY_SESSION", nil) {
		t.Fatal("stopped platform accepted another job")
	}
}

func TestFeedbackCards_NullPersistenceIsEmpty(t *testing.T) {
	for _, data := range []string{"null", `{"TEST_ONLY_NULL":null}`} {
		path := filepath.Join(t.TempDir(), "TEST_ONLY_feedback.json")
		if err := os.WriteFile(path, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
		cards := &feedbackCards{path: path}
		if _, ok := cards.lookup("TEST_ONLY_NULL"); ok {
			t.Fatal("null card was considered known")
		}
		cards.remember("TEST_ONLY_MESSAGE", "TEST_ONLY_SESSION", "TEST_ONLY_BODY", "", feedbackSections{Turn: true})
		if _, ok := cards.choose("TEST_ONLY_MESSAGE", "up", ""); !ok {
			t.Fatal("state could not recover after null persistence")
		}
	}
}

func TestTurnFeedback_ScoreCardUsesChatCreateForNonThread(t *testing.T) {
	p, fake := newFakeFeishuPlatform(t)
	p.sendTurnFeedbackScoreCard(context.Background(), "feishu:oc_TEST_ONLY_CHAT", []byte(`{"score_card":{"schema":"2.0","body":{"elements":[{"tag":"markdown","content":"TEST_ONLY_SCORE"}]}}}`))
	if len(fake.sent) != 1 || fake.sent[0]["path"] != "/open-apis/im/v1/messages" || fake.sent[0]["reply_in_thread"] != "false" {
		t.Fatal("non-thread score card did not use chat creation")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	p.sendTurnFeedbackScoreCard(ctx, "feishu:oc_TEST_ONLY_CHAT", []byte(`{"score_card":{}}`))
	p.sendTurnFeedbackScoreCard(context.Background(), "feishu:oc_TEST_ONLY_CHAT", []byte(strings.Repeat(" ", turnFeedbackOutputMax+1)))
	if len(fake.sent) != 1 {
		t.Fatal("canceled or oversized output caused a send")
	}
}

func TestTurnFeedback_FailedUpdatesNeverRememberUndeliveredContent(t *testing.T) {
	p, fake := newFakeFeishuPlatform(t)
	h := &feishuPreviewHandle{messageID: "om_TEST_ONLY_PREVIEW", chatID: "oc_TEST_ONLY_CHAT", sessionKey: "feishu:oc_TEST_ONLY_CHAT"}
	fake.failPatch = true
	if err := p.UpdateMessage(context.Background(), h, "TEST_ONLY_UNDELIVERED"); err == nil {
		t.Fatal("fixture did not reject preview update")
	}
	if len(p.lastReplyCards) != 0 {
		t.Fatal("failed preview was remembered")
	}
	if err := p.UpdateMessageWithStatusFooter(context.Background(), h, "TEST_ONLY_UNDELIVERED", ""); err == nil {
		t.Fatal("fixture did not reject final update")
	}
	if _, ok := p.feedbackCards.lookup(h.messageID); ok {
		t.Fatal("failed final card was persisted")
	}
}

func TestTurnFeedback_ResetAndCapturePreventStaleNoticeTargets(t *testing.T) {
	p, fake := newFakeFeishuPlatform(t)
	rc := replyContext{chatID: "oc_TEST_ONLY_CHAT", sessionKey: "feishu:oc_TEST_ONLY_CHAT"}
	ctx := context.Background()
	if err := p.Send(ctx, rc, "**TEST_ONLY_ANSWER**"); err != nil {
		t.Fatal(err)
	}
	p.CaptureTurnFeedbackAnswer(rc)
	if err := p.Send(ctx, rc, "**TEST_ONLY_PROGRESS**"); err != nil {
		t.Fatal(err)
	}
	if err := p.AttachTurnFeedback(ctx, rc); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(fake.patched[fake.sent[0]["id"]], "TEST_ONLY_ANSWER") || fake.patched[fake.sent[1]["id"]] != "" {
		t.Fatal("later progress replaced the captured answer")
	}
	p.BeginTurnFeedback(rc)
	fake.failSend = true
	if err := p.Send(ctx, rc, "**TEST_ONLY_FAILED_NEXT_ANSWER**"); err == nil {
		t.Fatal("fixture did not reject new answer")
	}
	if err := p.AttachTurnFeedback(ctx, rc); err != nil {
		t.Fatal(err)
	}
	if len(fake.patched) != 1 || len(fake.sent) != 2 {
		t.Fatal("failed next turn reused the previous target")
	}
}

func TestTurnFeedback_DeletedPreviewCannotBePatched(t *testing.T) {
	p, fake := newFakeFeishuPlatform(t)
	rc := replyContext{chatID: "oc_TEST_ONLY_CHAT", sessionKey: "feishu:oc_TEST_ONLY_CHAT"}
	h := &feishuPreviewHandle{messageID: "om_TEST_ONLY_PREVIEW", sessionKey: rc.sessionKey}
	p.rememberPreviewCard(h, "TEST_ONLY_ANSWER")
	p.CaptureTurnFeedbackAnswer(rc)
	if err := p.DeletePreviewMessage(context.Background(), h); err != nil {
		t.Fatal(err)
	}
	if err := p.AttachTurnFeedback(context.Background(), rc); err != nil {
		t.Fatal(err)
	}
	if len(fake.sent) != 0 || len(fake.patched) != 0 {
		t.Fatal("deleted preview was reused")
	}
}

func TestTurnFeedback_PlainFinalWithoutFooterStaysText(t *testing.T) {
	p, fake := newFakeFeishuPlatform(t)
	rc := replyContext{chatID: "oc_TEST_ONLY_CHAT", sessionKey: "feishu:oc_TEST_ONLY_CHAT"}
	if err := p.SendWithStatusFooter(context.Background(), rc, "TEST_ONLY_PLAIN_ANSWER", ""); err != nil {
		t.Fatal(err)
	}
	if len(fake.sent) != 2 || fake.sent[0]["msg_type"] != "text" || fake.sent[1]["msg_type"] != "interactive" {
		t.Fatal("expected unchanged text followed by feedback controls")
	}
}

func TestTurnFeedback_ConcurrentSubmissionStateMatchesCommandOrder(t *testing.T) {
	ip, _ := newTurnFeedbackPlatform(t)
	const count = 32
	commands := make(chan string, count)
	ip.turnFeedbackRun = func(_ context.Context, _ string, args []string) {
		commands <- args[3]
	}
	var submissions sync.WaitGroup
	for i := 0; i < count; i++ {
		submissions.Add(1)
		verdict := "up"
		if i%2 != 0 {
			verdict = "down"
		}
		go func(choice string) {
			defer submissions.Done()
			resp, err := feedbackClick(ip, choice, nil)
			if err != nil || resp == nil || resp.Toast.Type != "success" {
				t.Error("concurrent submission was rejected")
			}
		}(verdict)
	}
	submissions.Wait()
	last := ""
	for i := 0; i < count; i++ {
		select {
		case last = <-commands:
		case <-time.After(time.Second):
			t.Fatal("accepted command did not run")
		}
	}
	card, ok := ip.feedbackCards.lookup("om_TEST_ONLY_REPLY")
	if !ok || card.Verdict != last {
		t.Fatal("persisted choice disagrees with accepted execution order")
	}
}
