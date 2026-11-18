package wpsagentspace

import (
	"context"
	"errors"
	"net/http"
	"os"
	"testing"

	"github.com/chenhg5/cc-connect/internal/testutil"
	"github.com/gorilla/websocket"
)

var errOfflineWebSocket = errors.New("external WebSocket disabled in unit tests")

func TestMain(m *testing.M) {
	http.DefaultTransport = testutil.NewLocalHTTPTransport()
	websocket.DefaultDialer = testutil.NewLocalWebSocketDialer()
	// Startup/decryption tests do not exercise a real WebSocket service.
	// Keep this replacement installed until process exit, including background
	// workers that may outlive the test's immediate Stop call.
	dialWebSocket = func(context.Context, string, http.Header) (*websocket.Conn, *http.Response, error) {
		return nil, nil, errOfflineWebSocket
	}
	os.Exit(m.Run())
}

func TestConnectUsesOfflineWebSocketBoundary(t *testing.T) {
	// An empty URL stays non-networking even if the seam is accidentally bypassed.
	// The expected error proves connect reached our in-memory replacement.
	p := &Platform{}
	if err := p.connect(context.Background()); !errors.Is(err, errOfflineWebSocket) {
		t.Fatalf("connect error = %v, want the in-memory WebSocket boundary", err)
	}
}
