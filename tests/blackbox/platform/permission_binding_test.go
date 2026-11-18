package platform

import (
	"testing"
	"time"

	"github.com/chenhg5/cc-connect/core"
)

func TestMockPermissionActionKeepsBindingAndActor(t *testing.T) {
	p := New("test")
	got := make(chan *core.Message, 1)
	if err := p.Start(func(_ core.Platform, msg *core.Message) { got <- msg }); err != nil {
		t.Fatal(err)
	}
	payload := "perm:TEST_ONLY_PROMPT:allow"
	if err := p.InjectCardAction("member", "chat", payload); err != nil {
		t.Fatal(err)
	}
	select {
	case msg := <-got:
		if msg.Content != payload || !msg.IsPermissionResponse || msg.UserID != "member" || msg.SessionKey != p.SessionKeyFor("member", "chat") {
			t.Fatalf("permission binding or actor was lost: %+v", msg)
		}
	case <-time.After(time.Second):
		t.Fatal("permission action was not delivered")
	}
	for _, payload := range []string{"perm:allow", "perm::deny", "perm:TEST_ONLY_PROMPT:unknown"} {
		if err := p.InjectCardAction("member", "chat", payload); err == nil {
			t.Fatalf("accepted malformed callback %q", payload)
		}
	}
	select {
	case <-got:
		t.Fatal("malformed callback reached handler")
	default:
	}
}
