package core

import (
	"strings"
	"testing"
)

func TestParsePermissionCallback(t *testing.T) {
	for _, decision := range []string{"allow", "deny", "allow_all"} {
		for _, id := range []string{"prompt-current", strings.Repeat("A", 48)} {
			payload := "perm:" + id + ":" + decision
			gotID, gotDecision, ok := ParsePermissionCallback(payload)
			if !ok || gotID != id || gotDecision != decision || len(payload) > 64 {
				t.Fatal("bound callback did not round-trip within the byte limit")
			}
		}
	}
	for _, payload := range []string{
		"perm:allow", "perm::allow", "perm:current:unknown", "perm:current:allow:extra",
		"perm:with space:allow", "perm:with\nnewline:deny", "perm:非ASCII:allow",
		"perm:" + strings.Repeat("A", 49) + ":allow", "other:current:allow",
	} {
		if _, _, ok := ParsePermissionCallback(payload); ok {
			t.Fatal("malformed or legacy callback accepted")
		}
	}
}

func TestPermissionInlineButtonsBindEveryDecision(t *testing.T) {
	e := newTestEngine()
	p := &stubInlineButtonPlatform{stubPlatformEngine: stubPlatformEngine{n: "inline"}}
	e.sendPermissionPrompt(p, "ctx", "Permission requested", "read", "details", "prompt-current")
	var decisions []string
	for _, row := range p.buttonRows {
		for _, button := range row {
			id, decision, ok := ParsePermissionCallback(button.Data)
			if !ok || id != "prompt-current" {
				t.Fatal("inline button lost its current prompt binding")
			}
			decisions = append(decisions, decision)
		}
	}
	if strings.Join(decisions, ",") != "allow,deny,allow_all" {
		t.Fatal("permission decision set changed")
	}
}

func TestInlinePermissionAcknowledgesOnlySuccessfulSubmission(t *testing.T) {
	e := newTestEngine()
	p := &stubInlineButtonPlatform{stubPlatformEngine: stubPlatformEngine{n: "inline"}}
	agent := &recordingAgentSession{}
	e.interactiveStates["chat"] = &interactiveState{agentSession: agent, pending: &pendingPermission{
		RequestID: "request", CallbackID: "prompt-current", Resolved: make(chan struct{}),
	}}
	msg := &Message{SessionKey: "chat", IsPermissionResponse: true, ReplyCtx: "ctx"}
	e.handlePendingPermission(p, msg, "perm:prompt-stale:allow", "chat")
	if len(p.getSent()) != 0 || agent.calls != 0 {
		t.Fatal("stale click was acknowledged as an approval")
	}
	e.handlePendingPermission(p, msg, "perm:prompt-current:allow", "chat")
	if agent.calls != 1 || !sentContains(p.getSent(), e.i18n.T(MsgPermissionAllowed)) {
		t.Fatal("successful submission lacks engine confirmation")
	}
}
