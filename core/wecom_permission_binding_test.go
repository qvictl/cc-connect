package core

import "testing"

// The WeCom adapter must preserve the old card's request nonce. Keeping it on
// the wire lets core reject that card after a replacement prompt is installed.
// Adapter tests separately cover rendering Extra and decoding HTTP/WS events.
func TestWeComStaleBoundCardCannotAnswerReplacementPrompt(t *testing.T) {
	for _, sessionKey := range []string{"wecom:allowed", "wecom:chat:allowed"} {
		t.Run(sessionKey, func(t *testing.T) {
			e := newTestEngine()
			agent := &recordingAgentSession{}
			pending := &pendingPermission{RequestID: "reused-adapter-request", CallbackID: "current", Resolved: make(chan struct{})}
			state := &interactiveState{agentSession: agent, pending: pending}
			e.interactiveStates[sessionKey] = state
			p := &stubPlatformEngine{n: "wecom"}
			msg := &Message{SessionKey: sessionKey, Platform: "wecom", UserID: "allowed", IsPermissionResponse: true}
			if !e.handlePendingPermission(p, msg, "perm:old:allow_all", sessionKey) {
				t.Fatal("stale card escaped into ordinary input")
			}
			if agent.calls != 0 || state.pending != pending || state.approveAll {
				t.Fatal("old WeCom card changed the replacement prompt")
			}
			if !e.handlePendingPermission(p, msg, "perm:current:deny", sessionKey) {
				t.Fatal("current WeCom card was not handled")
			}
			if agent.calls != 1 || state.pending != nil || state.approveAll || agent.lastResult.Behavior != "deny" {
				t.Fatal("current WeCom card did not resolve exactly its prompt")
			}
		})
	}
}
