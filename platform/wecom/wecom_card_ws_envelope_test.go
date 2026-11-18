package wecom

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// This exercises serialization over a loopback mock only. It neither authenticates
// nor establishes live delivery semantics, especially for fire-and-forget ReplyCard.
func TestCardTaskIDWSEnvelopes(t *testing.T) {
	frames := make(chan wsFrame, 2)
	serverDone := make(chan error, 1)
	serverStopped := make(chan struct{})
	upgrader := websocket.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(serverStopped)
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			serverDone <- err
			return
		}
		defer conn.Close()
		if err := conn.SetReadDeadline(time.Now().Add(4 * time.Second)); err != nil {
			serverDone <- err
			return
		}
		if err := conn.SetWriteDeadline(time.Now().Add(4 * time.Second)); err != nil {
			serverDone <- err
			return
		}
		for i := 0; i < 2; i++ {
			var frame wsFrame
			if err := conn.ReadJSON(&frame); err != nil {
				serverDone <- err
				return
			}
			frames <- frame
			zero := 0
			if err := conn.WriteJSON(wsFrame{Headers: frame.Headers, ErrCode: &zero}); err != nil {
				serverDone <- err
				return
			}
		}
		serverDone <- nil
	}))
	t.Cleanup(server.Close)
	endpoint, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	if ip := net.ParseIP(endpoint.Hostname()); ip == nil || !ip.IsLoopback() {
		t.Fatal("mock endpoint is not loopback")
	}
	endpoint.Scheme = "ws"
	// No default dialer, proxy, environment credentials, or non-loopback fallback.
	dialer := websocket.Dialer{
		HandshakeTimeout: 2 * time.Second,
		NetDialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			if address != endpoint.Host {
				return nil, errors.New("non-mock websocket destination rejected")
			}
			return (&net.Dialer{Timeout: 2 * time.Second}).DialContext(ctx, network, address)
		},
	}
	conn, response, err := dialer.Dial(endpoint.String(), nil)
	if err != nil {
		if response != nil && response.Body != nil {
			_ = response.Body.Close()
		}
		t.Fatal(err)
	}
	p := &WSPlatform{conn: conn}
	readerDone := make(chan struct{})
	go func() {
		defer close(readerDone)
		for {
			var frame wsFrame
			if err := conn.ReadJSON(&frame); err != nil {
				return
			}
			p.handleFrame(frame)
		}
	}()
	t.Cleanup(func() {
		_ = conn.Close()
		select {
		case <-readerDone:
		case <-time.After(2 * time.Second):
			t.Error("mock websocket reader did not stop")
		}
		select {
		case <-serverStopped:
		case <-time.After(2 * time.Second):
			t.Error("mock websocket handler did not stop")
		}
	})
	if err := conn.SetWriteDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}
	// Shorter than the production five-second fallback: a missing ACK cannot be
	// mistaken for SendCard success via that timeout path in this mock.
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	title := "WebSocket review"
	if err := p.SendCard(ctx, wsReplyContext{chatID: "mock-chat"}, taskIDRegressionCard(&title)); err != nil {
		t.Fatal(err)
	}
	if err := p.ReplyCard(ctx, wsReplyContext{reqID: "mock-inbound-request", chatID: "mock-chat"}, taskIDRegressionCard(nil)); err != nil {
		t.Fatal(err)
	}
	seen := make(map[string]bool)
	for i := 0; i < 2; i++ {
		var frame wsFrame
		select {
		case frame = <-frames:
		case <-ctx.Done():
			t.Fatal("mock server did not capture card frame")
		}
		var body map[string]any
		if err := json.Unmarshal(frame.Body, &body); err != nil {
			t.Fatal(err)
		}
		wantTitle := title
		if i == 0 {
			if frame.Cmd != "aibot_send_msg" || !strings.HasPrefix(frame.Headers.ReqID, "aibot_send_msg") || body["chatid"] != "mock-chat" || len(body) != 3 {
				t.Fatal("unexpected proactive frame routing or body")
			}
		} else {
			wantTitle = "cc-connect"
			if frame.Cmd != "aibot_respond_msg" || frame.Headers.ReqID != "mock-inbound-request" || len(body) != 2 {
				t.Fatal("unexpected reply frame routing or body")
			}
			if _, exists := body["chatid"]; exists {
				t.Fatal("reply frame unexpectedly includes proactive chat routing")
			}
		}
		if body["msgtype"] != "template_card" {
			t.Fatal("unexpected websocket message type")
		}
		payload, ok := body["template_card"].(map[string]any)
		if !ok {
			t.Fatal("missing websocket template card")
		}
		id := assertTaskIDRegressionPayload(t, payload, wantTitle)
		if seen[id] {
			t.Fatal("websocket sends reused task_id")
		}
		seen[id] = true
	}
	select {
	case err := <-serverDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("mock websocket server did not complete")
	}
}
