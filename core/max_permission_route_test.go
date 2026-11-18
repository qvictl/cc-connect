package core

import "testing"

// A platform routing failure must not turn one chat's bound card into approval
// for a different pending prompt. The adapter's correct route must still work.
// This is an in-memory core guard, not a live MAX reproduction.
func TestBoundPermissionWrongChatCannotAnswerAnotherPrompt(t *testing.T) {
	e := newTestEngine()
	dialog := &recordingAgentSession{}
	group := &recordingAgentSession{}
	dialogPending := &pendingPermission{RequestID: "dialog-request", CallbackID: "dialog-prompt", Resolved: make(chan struct{})}
	groupPending := &pendingPermission{RequestID: "group-request", CallbackID: "group-prompt", Resolved: make(chan struct{})}
	dialogState := &interactiveState{agentSession: dialog, pending: dialogPending}
	groupState := &interactiveState{agentSession: group, pending: groupPending}
	e.interactiveStates["max:100:42"] = dialogState
	e.interactiveStates["max:200:42"] = groupState
	p := &stubPlatformEngine{n: "max"}
	payload := "perm:dialog-prompt:allow_all"

	misrouted := &Message{SessionKey: "max:200:42", Platform: "max", UserID: "42", IsPermissionResponse: true}
	if !e.handlePendingPermission(p, misrouted, payload, misrouted.SessionKey) {
		t.Fatal("wrong-chat permission escaped into ordinary input")
	}
	if dialog.calls != 0 || group.calls != 0 || dialogState.pending != dialogPending || groupState.pending != groupPending || groupState.approveAll {
		t.Fatal("wrong-chat callback changed a pending prompt")
	}

	correct := &Message{SessionKey: "max:100:42", Platform: "max", UserID: "42", IsPermissionResponse: true}
	if !e.handlePendingPermission(p, correct, payload, correct.SessionKey) {
		t.Fatal("correct dialog permission was not handled")
	}
	if dialog.calls != 1 || group.calls != 0 || dialogState.pending != nil || groupState.pending != groupPending || !dialogState.approveAll || groupState.approveAll {
		t.Fatal("correct dialog callback did not resolve only its own prompt")
	}
}
