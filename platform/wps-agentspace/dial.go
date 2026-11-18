package wpsagentspace

import (
	"context"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
)

// Lifecycle tests replace this boundary before any background worker starts.
// Production keeps the same per-connection dialer and handshake timeout.
var dialWebSocket = func(ctx context.Context, url string, header http.Header) (*websocket.Conn, *http.Response, error) {
	dialer := websocket.Dialer{HandshakeTimeout: 10 * time.Second}
	return dialer.DialContext(ctx, url, header)
}
