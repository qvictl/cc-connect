package wecom

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/chenhg5/cc-connect/core"
)

// No credentials, listener or network. HTTP event tests enter after the
// authenticated encrypted-envelope boundary, using decoded fixture XML.
type permissionNoNetwork struct{}

func (permissionNoNetwork) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, fmt.Errorf("permission fixture prohibits HTTP")
}

func permissionCardFixture(binding string) *core.Card {
	extra := map[string]string{"perm_request_id": binding}
	return core.NewCard().Title("Permission", "orange").ButtonsEqual(
		core.CardButton{Text: "Allow", Type: "primary", Value: "perm:allow", Extra: extra},
		core.CardButton{Text: "Deny", Type: "danger", Value: "perm:deny", Extra: extra},
	).Buttons(core.CardButton{Text: "Allow all", Value: "perm:allow_all", Extra: extra}).Build()
}

func TestWeComPermissionCardRejectsUnboundAndConflictingKeys(t *testing.T) {
	for _, tc := range []struct {
		key   string
		extra map[string]string
		want  string
	}{
		{"perm:allow", nil, ""},
		{" perm:allow", map[string]string{"perm_request_id": "current"}, ""},
		{"perm:deny", map[string]string{"perm_request_id": ""}, ""},
		{"perm:allow_all", map[string]string{"perm_request_id": "bad:binding"}, ""},
		{"perm:deny_all", map[string]string{"perm_request_id": "current"}, ""},
		{"perm:current:unknown", nil, ""},
		{"perm::allow", nil, ""},
		{"perm:current:allow:extra", nil, ""},
		{"perm:current:allow", map[string]string{"perm_request_id": "other"}, ""},
		{"perm:current:allow", map[string]string{"perm_request_id": ""}, ""},
		{"perm:current:allow", nil, "perm:current:allow"},
		{"perm:current:allow", map[string]string{"perm_request_id": "current"}, "perm:current:allow"},
		{"perm:allow_all", map[string]string{"perm_request_id": "current"}, "perm:current:allow_all"},
		{"act:/stop", map[string]string{"perm_request_id": "unused"}, "act:/stop"},
	} {
		t.Run(tc.key+tc.want, func(t *testing.T) {
			card := core.NewCard().Buttons(core.CardButton{Text: "Action", Value: tc.key, Extra: tc.extra}).Build()
			payload, err := buildWeComTemplateCard(card)
			if tc.want == "" {
				if err == nil || payload != nil {
					t.Fatal("invalid permission card was rendered")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if payload["button_list"].([]map[string]any)[0]["key"] != tc.want {
				t.Fatal("button key changed")
			}
		})
	}
}

func deliverPermissionFixture(t *testing.T, transport, key, actor, taskID string, out chan *core.Message) {
	t.Helper()
	if transport == "http" {
		p := &Platform{allowFrom: "allowed", apiClient: &http.Client{Transport: permissionNoNetwork{}}, handler: func(_ core.Platform, m *core.Message) { out <- m }}
		p.userNameCache.Store("allowed", "Allowed user")
		p.userNameCache.Store("other", "Other user")
		body := fmt.Sprintf(`<xml><MsgType>event</MsgType><Event>template_card_event</Event><FromUserName>%s</FromUserName><MsgId>7</MsgId><EventKey><![CDATA[%s]]></EventKey><TaskId>%s</TaskId></xml>`, actor, key, taskID)
		var event xmlMessage
		if err := xml.Unmarshal([]byte(body), &event); err != nil {
			t.Fatal(err)
		}
		p.handleTemplateCardEvent(event)
		return
	}
	p := &WSPlatform{allowFrom: "allowed", handler: func(_ core.Platform, m *core.Message) { out <- m }}
	// Exercise the flat event fields currently supported by this adapter.
	// Nested event schema compatibility is outside this binding correction.
	body, err := json.Marshal(map[string]any{
		"msgid": "event-7", "chatid": "chat", "from": map[string]string{"userid": actor}, "msgtype": "event",
		"event": map[string]string{"eventtype": "template_card_event", "event_key": key, "task_id": taskID},
	})
	if err != nil {
		t.Fatal(err)
	}
	p.handleEventCallback(wsFrame{Cmd: "aibot_event_callback", Headers: wsFrameHeaders{ReqID: "event-request"}, Body: body})
}

func TestWeComPermissionBindingCardToBothEventTransports(t *testing.T) {
	for _, transport := range []string{"http", "ws"} {
		for _, binding := range []string{"prompt-current", "prompt-stale"} {
			payload, err := buildWeComTemplateCard(permissionCardFixture(binding))
			if err != nil {
				t.Fatal(err)
			}
			buttons := payload["button_list"].([]map[string]any)
			for i, decision := range []string{"allow", "deny", "allow_all"} {
				t.Run(transport+binding+decision, func(t *testing.T) {
					key := buttons[i]["key"].(string)
					if key != "perm:"+binding+":"+decision {
						t.Fatalf("core Extra binding lost: %q", key)
					}
					out := make(chan *core.Message, 1)
					// Deliberately unrelated: task ID cannot replace the prompt nonce.
					deliverPermissionFixture(t, transport, key, "allowed", "unrelated-task", out)
					select {
					case m := <-out:
						wantSession := "wecom:allowed"
						if transport == "ws" {
							wantSession = "wecom:chat:allowed"
						}
						if m.Content != key || !m.IsPermissionResponse || m.UserID != "allowed" || m.SessionKey != wantSession {
							t.Fatalf("binding/actor lost: %+v", m)
						}
						id, gotDecision, valid := core.ParsePermissionCallback(m.Content)
						if !valid || id != binding || gotDecision != decision {
							t.Fatal("core cannot validate original binding")
						}
					case <-time.After(time.Second):
						t.Fatal("bound callback was not delivered")
					}
				})
			}
		}
	}
}

func TestWeComPermissionEventsFailClosedAndPreserveOrdinaryButtons(t *testing.T) {
	for _, transport := range []string{"http", "ws"} {
		for _, tc := range []struct { key, actor string; accept bool }{
			{"perm:allow", "allowed", false},
			{" perm:allow", "allowed", false},
			{"perm:current:allow ", "allowed", false},
			{"perm:deny", "allowed", false},
			{"perm:allow_all", "allowed", false},
			{"perm:deny_all", "allowed", false},
			{"perm::allow", "allowed", false},
			{"perm:current:unknown", "allowed", false},
			{"perm:current:allow:extra", "allowed", false},
			{"perm:current:allow", "other", false},
			{"perm:current:allow", "", false},
			{"act:/stop", "allowed", true},
			{"opaque button", "allowed", true},
		} {
			t.Run(transport+tc.key+tc.actor, func(t *testing.T) {
				out := make(chan *core.Message, 1)
				deliverPermissionFixture(t, transport, tc.key, tc.actor, "current", out)
				wait := 50 * time.Millisecond
				if tc.accept {
					wait = time.Second
				}
				select {
				case m := <-out:
					if !tc.accept || m.Content != tc.key || m.IsPermissionResponse || strings.HasPrefix(m.Content, "perm:") {
						t.Fatalf("invalid event dispatch: %+v", m)
					}
				case <-time.After(wait):
					if tc.accept {
						t.Fatal("ordinary button not delivered")
					}
				}
			})
		}
	}
}
