package feishu

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

// Optional turn feedback adds "👍 有用 / 👎 没用" to supported final replies
// and an optional comment box. Text replies use a separate controls card. A click runs `turn_feedback_exec` with the reply's
// message id and returns at once with a toast; nothing is posted to the
// chat and no turn starts. The overall-result buttons (成功 / 失败 / 终止)
// run the same command with --outcome; when its stdout is a JSON object
// {"score_card": <card>} that card is posted into the same conversation.
//
// Options (platform options of the feishu platform):
//
//	turn_feedback_exec   = "my-feedback-cmd"  # empty: no controls
//	turn_feedback_gate   = "/path/to/file"    # "off" in it hides 👍 / 👎 and the comment box
//	result_feedback_gate = "/path/to/file"    # "off" in it hides the result buttons
//	turn_feedback_state  = "/path/to/file"    # remembers the cards so a click redraws them
//
// The gates are read before every final reply, so whoever writes them
// switches the controls without restarting the bridge. A missing gate file
// is on. Without result_feedback_gate the result buttons follow
// turn_feedback_gate; with both parts off a reply carries no controls.

const (
	turnFeedbackActionPrefix = "fb:"
	turnFeedbackCommentMax   = 1000
	turnFeedbackTimeout      = 30 * time.Second
	turnFeedbackOutputMax    = 256 * 1024
	turnFeedbackStderrMax    = 16 * 1024
)

// feedbackSections says which parts of the controls a reply carries.
type feedbackSections struct {
	Turn   bool // 👍 / 👎 and the comment box
	Result bool // the overall result buttons
}

func (s feedbackSections) any() bool { return s.Turn || s.Result }

// gateOn reads a gate file: on unless it says "off". No file configured, or
// none written yet, is on.
func gateOn(path string) bool {
	if path == "" {
		return true
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return true
	}
	return strings.TrimSpace(string(data)) != "off"
}

// feedbackSections reads the gates: which parts a final reply sent now
// carries.
func (p *Platform) feedbackSections() feedbackSections {
	if strings.TrimSpace(p.turnFeedbackExec) == "" || !p.useInteractiveCard {
		return feedbackSections{}
	}
	turn := gateOn(p.turnFeedbackGate)
	result := turn
	if p.resultGate != "" {
		result = gateOn(p.resultGate)
	}
	return feedbackSections{Turn: turn, Result: result}
}

// TurnFeedbackEnabled implements core.TurnFeedbackPlatform: whether a final
// reply carries any of the controls.
func (p *Platform) TurnFeedbackEnabled() bool {
	return p.feedbackSections().any()
}

// turnFeedbackElement is the form appended to a final reply card, or nil
// when the controls are off, with the parts it carries. The session key
// rides in every button so the command learns which conversation the reply
// belongs to.
func (p *Platform) turnFeedbackElement(sessionKey string) (map[string]any, feedbackSections) {
	sections := p.feedbackSections()
	if !sections.any() {
		return nil, sections
	}
	return turnFeedbackForm(sessionKey, "", "", sections), sections
}

// turnFeedbackForm draws the controls -- the parts in sections. The last
// choice on this reply is highlighted -- filled, with a check -- and every
// button stays live, so a person can change their mind; the box keeps the
// last comment.
func turnFeedbackForm(sessionKey, verdict, comment string, sections feedbackSections) map[string]any {
	button := func(name, text, kind, choice string) map[string]any {
		if choice == verdict && verdict != "" {
			text = "✓ " + text
			kind += "_filled"
		} else if choice != "note" {
			kind = "default"
		}
		return map[string]any{
			"tag":              "button",
			"name":             name,
			"text":             map[string]any{"tag": "plain_text", "content": text},
			"type":             kind,
			"size":             "small",
			"form_action_type": "submit",
			"behaviors": []map[string]any{{
				"type": "callback",
				"value": map[string]any{
					"action":      turnFeedbackActionPrefix + choice,
					"session_key": sessionKey,
				},
			}},
		}
	}
	column := func(width string, elements ...map[string]any) map[string]any {
		col := map[string]any{
			"tag":            "column",
			"width":          width,
			"vertical_align": "center",
			"elements":       elements,
		}
		if width == "weighted" {
			col["weight"] = 1
		}
		return col
	}
	input := map[string]any{
		"tag":         "input",
		"name":        "comment",
		"placeholder": map[string]any{"tag": "plain_text", "content": "针对这次回复的意见（选填）"},
		"max_length":  turnFeedbackCommentMax,
		"width":       "fill",
	}
	if comment != "" {
		input["default_value"] = comment
	}
	var elements []map[string]any
	if sections.Turn {
		elements = append(elements,
			map[string]any{
				"tag":                "column_set",
				"horizontal_spacing": "8px",
				"columns": []map[string]any{
					column("weighted", map[string]any{
						"tag":       "markdown",
						"content":   "**这次回复有帮助吗？**",
						"text_size": "notation",
					}),
					column("auto", button("fb_up", "👍 有用", "primary", "up")),
					column("auto", button("fb_down", "👎 没用", "danger", "down")),
				},
			},
			map[string]any{
				"tag":                "column_set",
				"horizontal_spacing": "8px",
				"columns": []map[string]any{
					column("weighted", input),
					column("auto", button("fb_note", "提交意见", "default", "note")),
				},
			},
		)
	}
	if sections.Result {
		elements = append(elements, map[string]any{
			"tag":                "column_set",
			"horizontal_spacing": "8px",
			"columns": []map[string]any{
				column("weighted", map[string]any{"tag": "markdown", "content": "**任务整体结果评价**", "text_size": "notation"}),
				column("auto", button("fb_success", "成功", "primary", "success")),
				column("auto", button("fb_fail", "失败", "danger", "fail")),
				column("auto", button("fb_cancelled", "终止", "default", "cancelled")),
			},
		})
	}
	return map[string]any{
		"tag":      "form",
		"name":     "turn_feedback",
		"elements": elements,
	}
}

// onTurnFeedback answers a click on the controls. The reply is the card the
// click came from (messageID); the verdict is the button; the comment is
// whatever the box held.
func (p *Platform) onTurnFeedback(verdict string, formValue map[string]any, sessionKey string, callbackCtx *callback.Context, userID string) *callback.CardActionTriggerResponse {
	// Queue acceptance and the remembered highlight share one click order.
	// The command worker never holds this lock while executing a job.
	p.turnFeedbackSubmitMu.Lock()
	defer p.turnFeedbackSubmitMu.Unlock()
	// Bind the button to a successfully delivered, remembered message and the
	// authenticated callback destination. Never route a command from its
	// client-controlled session key alone. Unknown/expired cards fail closed.
	if callbackCtx == nil || callbackCtx.OpenChatID == "" || callbackCtx.OpenMessageID == "" || userID == "" {
		return turnFeedbackToast("error", "无法验证这条反馈，请重新生成回复卡片")
	}
	messageID := callbackCtx.OpenMessageID
	card, known := p.feedbackCards.lookup(messageID)
	parts := strings.SplitN(card.SessionKey, ":", 3)
	if !known || card.SessionKey != sessionKey || len(parts) < 2 || parts[0] != p.platformName || parts[1] != callbackCtx.OpenChatID {
		return turnFeedbackToast("error", "无法验证这条反馈，请重新生成回复卡片")
	}
	sections := card.sections()
	isResult := verdict == "success" || verdict == "fail" || verdict == "cancelled"
	if (isResult && !sections.Result) || (!isResult && !sections.Turn) {
		return turnFeedbackToast("error", "这条回复没有开启这类反馈")
	}
	comment, _ := formValue["comment"].(string)
	comment = strings.TrimSpace(comment)
	if utf8.RuneCountInString(comment) > turnFeedbackCommentMax {
		comment = string([]rune(comment)[:turnFeedbackCommentMax])
	}
	var said string
	outcome := ""
	switch verdict {
	case "success", "fail", "cancelled":
		outcome = verdict
		said = "任务整体结果已排队提交，成功后将显示评分卡"
	case "up":
		said = "已排队：👍 有用"
	case "down":
		said = "已排队：👎 没用"
	case "note":
		if comment == "" {
			return turnFeedbackToast("warning", "先写几句意见再提交")
		}
		verdict = ""
		said = "意见已排队提交"
	default:
		return nil
	}
	if strings.TrimSpace(p.turnFeedbackExec) == "" || !p.useInteractiveCard {
		return turnFeedbackToast("error", "这条回复没有开启反馈")
	}
	args := []string{"--message", messageID}
	if outcome != "" {
		args = append(args, "--outcome", outcome)
	} else if verdict != "" {
		args = append(args, "--verdict", verdict)
	}
	if comment != "" && outcome == "" {
		args = append(args, "--comment", comment)
	}
	if userID != "" {
		args = append(args, "--operator", userID)
	}
	run := p.turnFeedbackRun
	if run == nil {
		run = p.runTurnFeedback
	}
	// Feishu waits three seconds. Acknowledge queue acceptance, not command
	// completion. Rejected submissions must not redraw or persist a choice.
	if !p.enqueueTurnFeedback(run, sessionKey, args) {
		return turnFeedbackToast("error", "反馈暂时无法提交，请稍后重试")
	}
	resp := turnFeedbackToast("success", said)
	if outcome != "" {
		return resp
	}
	// Redraw the card with this choice highlighted, so it still shows
	// later which one was clicked.
	if card, ok := p.feedbackCards.choose(messageID, verdict, comment); ok {
		var data map[string]any
		cardJSON := buildFinalReplyCardJSON(card.Markdown, card.Footer, turnFeedbackForm(card.SessionKey, card.Verdict, card.Comment, card.sections()))
		if err := json.Unmarshal([]byte(cardJSON), &data); err == nil {
			resp.Card = &callback.Card{Type: "raw", Data: data}
		}
	} else {
		slog.Info(p.tag()+": turn feedback on a card sent before it was remembered; not redrawn", "message_id", messageID)
	}
	return resp
}

// turnFeedbackJob is owned by one worker, preserving accepted click order.
// Its context is canceled by Stop; the queue is never closed by producers.
type turnFeedbackJob struct {
	run func(context.Context, string, []string)
	sessionKey string
	args []string
}

func (p *Platform) enqueueTurnFeedback(run func(context.Context, string, []string), sessionKey string, args []string) bool {
	p.turnFeedbackMu.Lock()
	defer p.turnFeedbackMu.Unlock()
	if p.turnFeedbackStopped {
		return false
	}
	if p.turnFeedbackQueue == nil {
		ctx, cancel := context.WithCancel(context.Background())
		queue := make(chan turnFeedbackJob, 256)
		p.turnFeedbackCtx, p.turnFeedbackCancel, p.turnFeedbackQueue = ctx, cancel, queue
		go func() {
			for {
				select {
				case <-ctx.Done():
					return
				case job := <-queue:
					if ctx.Err() != nil {
						return
					}
					job.run(ctx, job.sessionKey, job.args)
				}
			}
		}()
	}
	job := turnFeedbackJob{run: run, sessionKey: sessionKey, args: append([]string(nil), args...)}
	select {
	case p.turnFeedbackQueue <- job:
		return true
	default:
		slog.Warn(p.tag()+": turn feedback queue full; click rejected")
		return false
	}
}

func (p *Platform) stopTurnFeedback() {
	p.turnFeedbackMu.Lock()
	defer p.turnFeedbackMu.Unlock()
	p.turnFeedbackStopped = true
	if p.turnFeedbackCancel != nil {
		p.turnFeedbackCancel()
	}
	p.turnFeedbackQueue = nil
}

// feedbackOutput stores only a bounded prefix while draining the pipe, so
// excessive stdout/stderr cannot grow memory or block the child on a pipe.
// Truncated stdout is never interpreted as a score card.
type feedbackOutput struct {
	data []byte
	limit int
	truncated bool
}

func (b *feedbackOutput) Write(data []byte) (int, error) {
	n := len(data)
	remaining := b.limit - len(b.data)
	if remaining < 0 {
		remaining = 0
	}
	if len(data) > remaining {
		data = data[:remaining]
		b.truncated = true
	}
	b.data = append(b.data, data...)
	return n, nil
}

// feedbackCommandEnv deliberately excludes inherited credentials, proxies,
// provider configuration and user HOME. This is not a process sandbox: the
// administrator must trust the configured executable and its filesystem use.
func feedbackCommandEnv(sessionKey, home, path string) []string {
	return []string{"PATH=" + path, "HOME=" + home, "LANG=C.UTF-8", "CC_SESSION_KEY=" + sessionKey}
}

// runTurnFeedback runs the configured trusted executable without a shell.
// The parent owns cancellation; neither child output nor command arguments
// (including comments) are written to logs.
func (p *Platform) runTurnFeedback(parent context.Context, sessionKey string, args []string) {
	fields := strings.Fields(p.turnFeedbackExec)
	if len(fields) == 0 || parent.Err() != nil {
		return
	}
	ctx, cancel := context.WithTimeout(parent, turnFeedbackTimeout)
	defer cancel()
	home, err := os.MkdirTemp("", "cc-connect-feedback-")
	if err != nil {
		slog.Warn(p.tag()+": turn feedback command setup failed")
		return
	}
	defer os.RemoveAll(home)
	cmd := exec.CommandContext(ctx, fields[0], append(fields[1:], args...)...)
	cmd.Env = feedbackCommandEnv(sessionKey, home, os.Getenv("PATH"))
	// Bound waiting for inherited pipes if a descendant outlives the child.
	cmd.WaitDelay = time.Second
	out := feedbackOutput{limit: turnFeedbackOutputMax}
	stderr := feedbackOutput{limit: turnFeedbackStderrMax}
	cmd.Stdout, cmd.Stderr = &out, &stderr
	if err := cmd.Run(); err != nil {
		slog.Warn(p.tag()+": turn feedback command failed", "cancelled", ctx.Err() != nil)
		return
	}
	if out.truncated {
		slog.Warn(p.tag()+": turn feedback command output exceeded limit")
		return
	}
	p.sendTurnFeedbackScoreCard(ctx, sessionKey, out.data)
}

func (p *Platform) sendTurnFeedbackScoreCard(ctx context.Context, sessionKey string, out []byte) {
	if ctx.Err() != nil || len(out) > turnFeedbackOutputMax {
		return
	}
	var result struct {
		ScoreCard json.RawMessage `json:"score_card"`
	}
	if json.Unmarshal(out, &result) != nil || len(result.ScoreCard) == 0 || string(result.ScoreCard) == "null" {
		return
	}
	rctx, err := p.ReconstructReplyCtx(sessionKey)
	if err != nil {
		slog.Warn(p.tag()+": result feedback reply context invalid")
		return
	}
	var card map[string]any
	if json.Unmarshal(result.ScoreCard, &card) != nil || card == nil {
		slog.Warn(p.tag()+": invalid result score card")
		return
	}
	bindScoreCardSession(card, sessionKey)
	content, err := json.Marshal(card)
	if err != nil {
		slog.Warn(p.tag()+": result score card encoding failed")
		return
	}
	// A chat-level session has no message ID; use the create API rather than
	// issuing an invalid reply request. Thread sessions keep their root.
	if _, err := p.sendCardID(ctx, rctx.(replyContext), string(content)); err != nil {
		slog.Warn(p.tag()+": result score card could not be sent; click again to retry")
	}
}

// Score cards come raw from the feedback command; their callbacks must keep
// the conversation just like callbacks produced by renderCardMap.
func bindScoreCardSession(value any, sessionKey string) {
	switch node := value.(type) {
	case map[string]any:
		if node["type"] == "callback" {
			if callbackValue, ok := node["value"].(map[string]any); ok {
				callbackValue["session_key"] = sessionKey
			}
		}
		for _, child := range node {
			bindScoreCardSession(child, sessionKey)
		}
	case []any:
		for _, child := range node {
			bindScoreCardSession(child, sessionKey)
		}
	}
}

// lastReplyCard is the last reply card a session was sent, kept so the
// controls can be patched onto it.
type lastReplyCard struct {
	messageID string
	markdown  string
	preview   *feishuPreviewHandle // set for a streamed preview card
}

func hasResolvedMention(content string) bool {
	return strings.Contains(content, `<at user_id=`) || strings.Contains(content, `<at id=`)
}

// sendReplyContent sends a reply exactly the way Send and Reply always have,
// and remembers it when it went out as a card: an agent often answers and
// then makes one last tool call (e.g. to close a ticket), so its answer has
// already gone out by the time the turn ends, and AttachTurnFeedback puts
// the controls on it. Text stays text; under a text answer the controls
// come as a card of their own.
func (p *Platform) sendReplyContent(ctx context.Context, rc replyContext, content string, reply bool) error {
	msgType, msgBody := buildReplyContent(content)
	markdown := ""
	if msgType == larkim.MsgTypeInteractive && p.TurnFeedbackEnabled() {
		markdown = sanitizeMarkdownURLs(preprocessFeishuMarkdown(content))
	}
	var messageID string
	var err error
	if reply {
		messageID, err = p.replyMessageID(ctx, rc, msgType, msgBody, "")
	} else {
		if rc.chatID == "" {
			return fmt.Errorf("%s: chatID is empty, cannot send new message", p.tag())
		}
		messageID, err = p.createMessageIDWithReceiveID(ctx, rc.chatID, rc.receiveIDType, msgType, msgBody, "send", "")
	}
	if err == nil {
		p.rememberReplyCard(rc.sessionKey, messageID, markdown)
	}
	return err
}

// rememberReplyCard records the session's last reply; anything that is not
// a patchable card clears the record, so a stale card is never patched.
func (p *Platform) rememberReplyCard(sessionKey, messageID, markdown string) {
	if sessionKey == "" {
		return
	}
	p.lastReplyMu.Lock()
	defer p.lastReplyMu.Unlock()
	if messageID == "" || markdown == "" {
		delete(p.lastReplyCards, sessionKey)
		return
	}
	if p.lastReplyCards == nil {
		p.lastReplyCards = map[string]lastReplyCard{}
	}
	p.lastReplyCards[sessionKey] = lastReplyCard{messageID: messageID, markdown: markdown}
}

// rememberPreviewCard records a text preview card as the session's last
// reply.
func (p *Platform) rememberPreviewCard(h *feishuPreviewHandle, markdown string) {
	if h == nil || h.sessionKey == "" || markdown == "" || !p.TurnFeedbackEnabled() {
		return
	}
	p.lastReplyMu.Lock()
	defer p.lastReplyMu.Unlock()
	if p.lastReplyCards == nil {
		p.lastReplyCards = map[string]lastReplyCard{}
	}
	p.lastReplyCards[h.sessionKey] = lastReplyCard{messageID: h.messageID, markdown: markdown, preview: h}
}

// AttachTurnFeedback implements core.TurnFeedbackAttacher: the turn's answer
// already went out, so the controls go onto that last reply card, or, when
// it cannot take them (text with a mention, a post), into a card of their
// own right under it.
func (p *Platform) AttachTurnFeedback(ctx context.Context, rctx any) error {
	rc, ok := rctx.(replyContext)
	if !ok {
		return fmt.Errorf("%s: invalid reply context type %T", p.tag(), rctx)
	}
	form, sections := p.turnFeedbackElement(rc.sessionKey)
	if form == nil {
		return nil
	}
	p.lastReplyMu.Lock()
	last, found := p.turnFeedbackAnswers[rc.sessionKey]
	delete(p.turnFeedbackAnswers, rc.sessionKey)
	delete(p.lastReplyCards, rc.sessionKey)
	p.lastReplyMu.Unlock()
	if !found {
		return nil
	}
	if last.messageID != "" {
		cardJSON := buildFinalReplyCardJSON(last.markdown, "", form)
		var err error
		cardID := ""
		if last.preview != nil {
			last.preview.mu.Lock()
			cardID = last.preview.cardID
			last.preview.mu.Unlock()
		}
		if cardID != "" {
			err = p.updateCardEntity(ctx, last.preview, cardJSON)
		} else {
			err = p.patchCardMessage(ctx, last.messageID, cardJSON)
		}
		if err == nil {
			p.feedbackCards.remember(last.messageID, rc.sessionKey, last.markdown, "", sections)
			slog.Info(p.tag()+": turn feedback controls added to the last reply", "message_id", last.messageID)
			return nil
		}
		slog.Warn(p.tag()+": could not add feedback controls to the last reply; sending them on their own", "message_id", last.messageID, "error", err)
	}
	return p.sendFeedbackControls(ctx, rc, form, sections)
}

// sendFeedbackControls sends the controls as a card of their own, right
// under an answer that could not carry them.
func (p *Platform) sendFeedbackControls(ctx context.Context, rc replyContext, form map[string]any, sections feedbackSections) error {
	messageID, err := p.sendCardID(ctx, rc, buildFinalReplyCardJSON("", "", form))
	if err == nil {
		p.feedbackCards.remember(messageID, rc.sessionKey, "", "", sections)
		slog.Info(p.tag()+": turn feedback controls sent on their own", "session", rc.sessionKey, "message_id", messageID)
	}
	return err
}

// sendCardID sends a card the way replies go -- in the thread when there is
// one -- and returns its message id.
func (p *Platform) sendCardID(ctx context.Context, rc replyContext, cardJSON string) (string, error) {
	if p.shouldUseThreadOrReplyAPI(rc) {
		return p.replyMessageID(ctx, rc, larkim.MsgTypeInteractive, cardJSON, "")
	}
	if rc.chatID == "" {
		return "", fmt.Errorf("%s: chatID is empty, cannot send new message", p.tag())
	}
	return p.createMessageIDWithReceiveID(ctx, rc.chatID, rc.receiveIDType, larkim.MsgTypeInteractive, cardJSON, "send", "")
}

func turnFeedbackToast(kind, content string) *callback.CardActionTriggerResponse {
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: kind, Content: content},
	}
}

// BeginTurnFeedback drops the preceding turn's patch target. The engine calls
// this for initial and queued turns; persisted feedback on old cards survives.
func (p *Platform) BeginTurnFeedback(rctx any) {
	if rc, ok := rctx.(replyContext); ok {
		p.lastReplyMu.Lock()
		delete(p.lastReplyCards, rc.sessionKey)
		delete(p.turnFeedbackAnswers, rc.sessionKey)
		p.lastReplyMu.Unlock()
	}
}

// CaptureTurnFeedbackAnswer snapshots only a successfully delivered answer.
// Later progress, notices and thinking messages must not replace that target.
// A zero card records a text answer, which needs separate controls.
func (p *Platform) CaptureTurnFeedbackAnswer(rctx any) {
	rc, ok := rctx.(replyContext)
	if !ok || rc.sessionKey == "" || !p.TurnFeedbackEnabled() {
		return
	}
	p.lastReplyMu.Lock()
	defer p.lastReplyMu.Unlock()
	if p.turnFeedbackAnswers == nil {
		p.turnFeedbackAnswers = map[string]lastReplyCard{}
	}
	p.turnFeedbackAnswers[rc.sessionKey] = p.lastReplyCards[rc.sessionKey]
}

func (p *Platform) forgetReplyCard(sessionKey, messageID string) {
	p.lastReplyMu.Lock()
	defer p.lastReplyMu.Unlock()
	if last, ok := p.lastReplyCards[sessionKey]; ok && last.messageID == messageID {
		delete(p.lastReplyCards, sessionKey)
	}
	if answer, ok := p.turnFeedbackAnswers[sessionKey]; ok && answer.messageID == messageID {
		delete(p.turnFeedbackAnswers, sessionKey)
	}
}
