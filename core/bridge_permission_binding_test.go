package core

import (
	"encoding/json"
	"testing"
	"time"
)

func TestBridgePermissionCallbackPreservesBinding(t *testing.T) {
	bs := NewBridgeServerInsecure(0, "", "/bridge/ws", nil)
	bp := bs.NewPlatform("test")
	e := NewEngine("test", &stubAgent{}, []Platform{bp}, "", LangEnglish)
	bs.RegisterEngine("test", e, bp)
	a := &bridgeAdapter{server: bs, platform: "test"}
	got := make(chan *Message, 4)
	bp.handler = func(_ Platform, msg *Message) { got <- msg }
	click := func(action string) {
		raw, err := json.Marshal(map[string]string{"session_key": "chat", "action": action})
		if err != nil {
			t.Fatal(err)
		}
		a.handleCardAction(raw)
	}
	for _, decision := range []string{"allow", "deny", "allow_all"} {
		payload := "perm:TEST_ONLY_PROMPT:" + decision
		click(payload)
		select {
		case msg := <-got:
			if msg.Content != payload || !msg.IsPermissionResponse || msg.SessionKey != "chat" || msg.UserID != "web-admin" {
				t.Fatalf("bridge lost binding or trusted identity: %+v", msg)
			}
		case <-time.After(time.Second):
			t.Fatal("bound permission click was not dispatched")
		}
	}
	for _, invalid := range []string{"perm:allow", "perm:deny", "perm:allow_all", "perm::allow", "perm:TEST_ONLY_PROMPT:unknown"} {
		click(invalid)
	}
	select {
	case <-got:
		t.Fatal("legacy or malformed permission escaped into a message")
	default:
	}
}

func TestBridgePermissionCallbackCannotAnswerDifferentPrompt(t *testing.T) {
	bs := NewBridgeServerInsecure(0, "", "/bridge/ws", nil)
	bp := bs.NewPlatform("test")
	e := newTestEngine()
	bs.RegisterEngine("test", e, bp)
	agent := &recordingAgentSession{}
	pending := &pendingPermission{RequestID: "request", CallbackID: "TEST_ONLY_CURRENT", Resolved: make(chan struct{})}
	state := &interactiveState{agentSession: agent, pending: pending}
	e.interactiveStates["chat"] = state
	processed := make(chan bool, 1)
	bp.handler = func(p Platform, msg *Message) {
		processed <- e.handlePendingPermission(p, msg, msg.Content, msg.SessionKey)
	}
	a := &bridgeAdapter{server: bs, platform: "test"}
	for i, binding := range []string{"TEST_ONLY_OLD", "TEST_ONLY_CURRENT"} {
		raw, err := json.Marshal(map[string]string{"session_key": "chat", "action": "perm:" + binding + ":allow"})
		if err != nil {
			t.Fatal(err)
		}
		a.handleCardAction(raw)
		select {
		case handled := <-processed:
			if !handled || agent.calls != i {
				t.Fatalf("click %d: handled=%v calls=%d", i, handled, agent.calls)
			}
		case <-time.After(time.Second):
			t.Fatal("permission handler did not finish")
		}
		if i == 0 && state.pending != pending {
			t.Fatal("stale callback changed the current pending prompt")
		}
	}
}
