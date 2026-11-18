package cloudweb

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/chenhg5/cc-connect/core"
)

func TestPermissionCardActionPreservesBinding(t *testing.T) {
	for _, action := range []string{"perm:prompt-current:allow", "perm:prompt-stale:deny", "perm:prompt-current:allow_all"} {
		got := make(chan *core.Message, 1)
		p := &Platform{handler: func(_ core.Platform, msg *core.Message) { got <- msg }}
		raw, _ := json.Marshal(wireCardAction{SessionKey: "cloud_web:chat:user", ReplyCtx: "context", Action: action})
		p.handleCardAction(raw)
		select {
		case msg := <-got:
			if !msg.IsPermissionResponse || msg.Content != action { t.Fatal("binding lost") }
		case <-time.After(time.Second): t.Fatal("callback not forwarded")
		}
	}
	for _, action := range []string{"perm:allow", "perm::allow", "perm:prompt:unknown", "perm:prompt:allow:extra"} {
		got := make(chan *core.Message, 1)
		p := &Platform{handler: func(_ core.Platform, msg *core.Message) { got <- msg }}
		raw, _ := json.Marshal(wireCardAction{SessionKey: "cloud_web:chat:user", ReplyCtx: "context", Action: action})
		p.handleCardAction(raw)
		select { case <-got: t.Fatal("invalid callback forwarded"); case <-time.After(time.Millisecond): }
	}
}
