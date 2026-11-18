package testutil

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/websocket"
)

func TestLocalWebSocketDialerRejectsExternalBeforeConnecting(t *testing.T) {
	for _, address := range []string{
		"ws://service.invalid/path", "wss://192.0.2.1/path",
		"ws://localhost/path", "ws://127.0.0.1.example.invalid/path",
		"wss://[2001:db8::1]/path",
	} {
		t.Run(address, func(t *testing.T) {
			conn, response, err := NewLocalWebSocketDialer().DialContext(context.Background(), address, nil)
			if conn != nil {
				_ = conn.Close()
			}
			if !errors.Is(err, errExternalSocket) || conn != nil || response != nil {
				t.Fatal("expected in-memory WebSocket rejection before connecting")
			}
		})
	}
}

func TestLocalWebSocketDialerAllowsLoopbackFixture(t *testing.T) {
	upgrader := websocket.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		_ = conn.Close()
	}))
	defer server.Close()

	dialer := NewLocalWebSocketDialer()
	if dialer.Proxy != nil {
		t.Fatal("local fixtures must not use a proxy")
	}
	address := "ws" + strings.TrimPrefix(server.URL, "http")
	conn, _, err := dialer.DialContext(context.Background(), address, nil)
	if err != nil {
		t.Fatal("could not connect to loopback WebSocket fixture")
	}
	_ = conn.Close()
}
