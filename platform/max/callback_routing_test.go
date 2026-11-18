package max

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chenhg5/cc-connect/core"
)

// No listener, token, CLI, or default transport: every HTTP operation stays
// in memory, including asynchronous mark_seen calls from handleMessage.
type callbackRoutingTransport struct {
	mu       sync.Mutex
	requests []string
}

func (r *callbackRoutingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Header.Get("Authorization") != "" {
		return nil, fmt.Errorf("callback fixture forbids credentials")
	}
	r.mu.Lock()
	r.requests = append(r.requests, req.Method+" "+req.URL.RequestURI())
	r.mu.Unlock()
	return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"message":{"body":{"mid":"dm-card"}}}`)), Request: req}, nil
}

func (r *callbackRoutingTransport) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.requests)
}

func newCallbackRoutingPlatform() (*Platform, *callbackRoutingTransport) {
	r := &callbackRoutingTransport{}
	return &Platform{apiBase: "https://max.invalid", client: &http.Client{Transport: r}}, r
}

func dispatchCallbackJSON(t *testing.T, p *Platform, body string) {
	t.Helper()
	var update maxUpdate
	if err := json.Unmarshal([]byte(body), &update); err != nil {
		t.Fatal(err)
	}
	p.handleUpdate(context.Background(), &update)
}

func callbackSourceJSON(kind string, chat, recipient int64, mid string) string {
	return fmt.Sprintf(`{"sender":{"user_id":900},"recipient":{"chat_type":%q,"chat_id":%d,"user_id":%d},"body":{"mid":%q,"text":"card"}}`, kind, chat, recipient, mid)
}

func callbackEnvelopeJSON(id, payload string, actor int64, source string) string {
	return fmt.Sprintf(`{"update_type":"message_callback","callback":{"callback_id":%q,"payload":%q,"user":{"user_id":%d,"name":"clicker"}},"message":%s}`, id, payload, actor, source)
}

func TestCallbackRoutingDMCardThenGroupMessage(t *testing.T) {
	for _, kind := range []string{"chat", "channel", "", "unexpected"} {
		t.Run(kind, func(t *testing.T) {
			p, transport := newCallbackRoutingPlatform()
			var got []*core.Message
			p.handler = func(_ core.Platform, m *core.Message) { got = append(got, m) }
			message := func(id, kind string, chat int64) {
				dispatchCallbackJSON(t, p, fmt.Sprintf(`{"update_type":"message_created","message":{"sender":{"user_id":42},"recipient":{"chat_type":%q,"chat_id":%d,"user_id":900},"timestamp":%d,"body":{"mid":%q,"text":"hello"}}}`, kind, chat, time.Now().UnixMilli(), id))
			}
			message("dm-input", "dialog", 10)
			if len(got) != 1 {
				t.Fatal("DM input was not dispatched")
			}
			payload := "perm:prompt-current:allow"
			if err := p.SendWithButtons(context.Background(), got[0].ReplyCtx, "permission", [][]core.ButtonOption{{{Text: "Allow", Data: payload}}}); err != nil {
				t.Fatal(err)
			}
			message("other-input", kind, 20)
			known, ok := p.chatOfUser.Load("42")
			if !ok || known != "10" {
				t.Fatalf("non-dialog polluted cache: %v", known)
			}
			got = nil
			dispatchCallbackJSON(t, p, callbackEnvelopeJSON("dm-click", payload, 42, callbackSourceJSON("dialog", 0, 42, "dm-card")))
			if len(got) != 1 {
				t.Fatalf("callback count = %d", len(got))
			}
			m := got[0]
			rctx, ok := m.ReplyCtx.(replyContext)
			if !ok || m.SessionKey != "max:10:42" || m.UserID != "42" || m.MessageID != "dm-click" || rctx.messageID != "dm-card" || rctx.chatID != "10" || rctx.userID != "42" || !m.IsPermissionResponse || m.Content != payload {
				t.Fatalf("wrong DM origin/identity/binding: %+v, context %+v", m, rctx)
			}
			if err := p.Send(context.Background(), rctx, "reply"); err != nil {
				t.Fatal(err)
			}
			transport.mu.Lock()
			defer transport.mu.Unlock()
			posts := 0
			for _, req := range transport.requests {
				if strings.HasPrefix(req, "PUT ") {
					t.Fatalf("premature edit: %s", req)
				}
				if strings.HasPrefix(req, "POST /messages?") {
					posts++
					if req != "POST /messages?chat_id=10" {
						t.Fatalf("wrong card/reply target: %s", req)
					}
				}
			}
			if posts != 2 {
				t.Fatalf("card and reply posts = %d", posts)
			}
		})
	}
}

func TestCallbackRoutingSourceAndActor(t *testing.T) {
	for _, kind := range []string{"dialog", "chat", "channel"} {
		for _, payload := range []string{"play", "perm:prompt-current:allow_all", "perm:prompt-current:deny"} {
			t.Run(kind+payload, func(t *testing.T) {
				p, transport := newCallbackRoutingPlatform()
				p.allowFrom = "42"
				p.chatOfUser.Store("42", "10")
				var got []*core.Message
				p.handler = func(_ core.Platform, m *core.Message) { got = append(got, m) }
				recipient := int64(42)
				if kind != "dialog" {
					recipient = 900
				}
				body := callbackEnvelopeJSON("click", payload, 42, callbackSourceJSON(kind, 20, recipient, "original-card"))
				dispatchCallbackJSON(t, p, body)
				dispatchCallbackJSON(t, p, body)
				if len(got) != 1 || got[0].SessionKey != "max:20:42" || got[0].UserID != "42" || got[0].Content != payload || got[0].IsPermissionResponse != strings.HasPrefix(payload, "perm:") {
					t.Fatalf("source, actor, payload or dedup lost: %+v", got)
				}
				if got[0].ReplyCtx.(replyContext).messageID != "original-card" {
					t.Fatal("original mid lost")
				}
				if transport.count() != 0 {
					t.Fatal("callback made premature HTTP request")
				}
			})
		}
	}
}

func TestCallbackRoutingDoesNotUseNestedMessage(t *testing.T) {
	p, transport := newCallbackRoutingPlatform()
	p.chatOfUser.Store("42", "10")
	p.handler = func(core.Platform, *core.Message) { t.Fatal("nested callback.message accepted as origin") }
	// The old fixture shape is not MAX's update contract; a populated nested
	// message cannot compensate for a missing/deleted top-level message.
	dispatchCallbackJSON(t, p, `{"update_type":"message_callback","callback":{"callback_id":"nested","payload":"perm:prompt-current:allow","user":{"user_id":42},"message":{"recipient":{"chat_type":"dialog","chat_id":10,"user_id":42},"body":{"mid":"card"}}}}`)
	if transport.count() != 0 {
		t.Fatal("unverified nested origin made an HTTP request")
	}
}

func TestCallbackRoutingFailsClosed(t *testing.T) {
	for _, tc := range []struct {
		name, id, source, allow string
		actor                   int64
		cache                   bool
	}{
		{"missing source", "c", "null", "", 42, true},
		{"missing mid", "c", callbackSourceJSON("dialog", 10, 42, ""), "", 42, true},
		{"blank mid", "c", callbackSourceJSON("dialog", 10, 42, " "), "", 42, true},
		{"unknown type", "c", callbackSourceJSON("future", 10, 42, "m"), "", 42, true},
		{"missing type", "c", callbackSourceJSON("", 10, 42, "m"), "", 42, true},
		{"group missing chat", "c", callbackSourceJSON("chat", 0, 42, "m"), "", 42, true},
		{"channel missing chat", "c", callbackSourceJSON("channel", 0, 42, "m"), "", 42, true},
		{"DM no cache", "c", callbackSourceJSON("dialog", 0, 42, "m"), "", 42, false},
		{"DM different recipient", "c", callbackSourceJSON("dialog", 10, 43, "m"), "", 42, true},
		{"missing actor", "c", callbackSourceJSON("dialog", 10, 42, "m"), "", 0, true},
		{"negative actor", "c", callbackSourceJSON("dialog", 10, 42, "m"), "", -1, true},
		{"blank callback", " ", callbackSourceJSON("dialog", 10, 42, "m"), "", 42, true},
		{"sender authorized not actor", "c", callbackSourceJSON("chat", 10, 900, "m"), "900", 42, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, transport := newCallbackRoutingPlatform()
			p.allowFrom = tc.allow
			if tc.cache {
				p.chatOfUser.Store("42", "10")
			}
			p.handler = func(core.Platform, *core.Message) { t.Fatal("unverified callback dispatched") }
			for i, payload := range []string{"play", "perm:prompt-current:allow"} {
				id := tc.id
				if strings.TrimSpace(id) != "" {
					id = fmt.Sprintf("%s%d", id, i)
				}
				dispatchCallbackJSON(t, p, callbackEnvelopeJSON(id, payload, tc.actor, tc.source))
			}
			if transport.count() != 0 {
				t.Fatal("rejected callback made an HTTP request")
			}
		})
	}
}

func TestCallbackRoutingOptionalRecipientAndSignedChatID(t *testing.T) {
	for _, tc := range []struct {
		name, source, want string
	}{
		{"null recipient", `{"recipient":{"chat_type":"dialog","chat_id":10,"user_id":null},"body":{"mid":"card"}}`, "max:10:42"},
		{"missing recipient", `{"recipient":{"chat_type":"dialog","chat_id":10},"body":{"mid":"card"}}`, "max:10:42"},
		{"negative chat", callbackSourceJSON("chat", -20, 0, "card"), "max:-20:42"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, transport := newCallbackRoutingPlatform()
			var got *core.Message
			p.handler = func(_ core.Platform, msg *core.Message) { got = msg }
			dispatchCallbackJSON(t, p, callbackEnvelopeJSON("click", "play", 42, tc.source))
			if got == nil || got.SessionKey != tc.want || got.UserID != "42" {
				t.Fatalf("valid optional/signed origin lost: %+v", got)
			}
			if transport.count() != 0 {
				t.Fatal("routing made an HTTP request")
			}
		})
	}
}

func TestCallbackRoutingRejectedOriginDoesNotConsumeDedup(t *testing.T) {
	p, transport := newCallbackRoutingPlatform()
	count := 0
	p.handler = func(_ core.Platform, _ *core.Message) { count++ }
	dispatchCallbackJSON(t, p, callbackEnvelopeJSON("same-click", "play", 42, "null"))
	if count != 0 {
		t.Fatal("invalid origin dispatched")
	}
	valid := callbackEnvelopeJSON("same-click", "play", 42, callbackSourceJSON("chat", 20, 0, "card"))
	dispatchCallbackJSON(t, p, valid)
	dispatchCallbackJSON(t, p, valid)
	if count != 1 || transport.count() != 0 {
		t.Fatal("rejected origin consumed dedup, or valid retry dispatched twice")
	}
}
