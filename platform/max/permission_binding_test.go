package max

import (
	"context"
	"testing"

	"github.com/chenhg5/cc-connect/core"
)

func TestPermissionCallbackPreservesBindingWithoutPrematureEdit(t *testing.T) {
	for _, payload := range []string{"perm:prompt-current:allow", "perm:prompt-stale:deny", "perm:prompt-current:allow_all"} {
		p, transport := newCallbackRoutingPlatform()
		p.allowFrom = "42"
		var got *core.Message
		p.handler = func(_ core.Platform, msg *core.Message) { got = msg }
		p.handleCallback(context.Background(), &maxCallback{CallbackID: payload, Payload: payload, User: maxUser{UserID: 42}}, &maxMessage{Recipient: maxRecipient{ChatType: "dialog", ChatID: 10, UserID: 42}, Body: maxBody{Mid: "card"}})
		if got == nil || got.Content != payload || !got.IsPermissionResponse {
			t.Fatal("binding lost")
		}
		if transport.count() != 0 {
			t.Fatal("permission card repainted before acceptance")
		}
	}
	for _, payload := range []string{"perm:allow", "perm::deny", "perm:prompt:unknown", "perm:prompt:allow:extra"} {
		p, transport := newCallbackRoutingPlatform()
		p.handler = func(core.Platform, *core.Message) { t.Fatal("invalid permission dispatched") }
		p.handleCallback(context.Background(), &maxCallback{CallbackID: payload, Payload: payload, User: maxUser{UserID: 42}}, &maxMessage{Recipient: maxRecipient{ChatType: "dialog", ChatID: 10, UserID: 42}, Body: maxBody{Mid: "card"}})
		if transport.count() != 0 {
			t.Fatal("invalid permission made an HTTP request")
		}
	}
}
