package testutil

import (
	"context"
	"errors"
	"net"
	"time"

	"github.com/gorilla/websocket"
)

var errExternalSocket = errors.New("external socket disabled in unit tests")

// NewLocalWebSocketDialer rejects non-loopback addresses before DNS or a socket
// connection. It never consults proxy environment variables. Tests that use a
// custom dialer still need their own in-memory or literal-loopback boundary.
func NewLocalWebSocketDialer() *websocket.Dialer {
	return &websocket.Dialer{
		NetDialContext:   dialLocalContext,
		HandshakeTimeout: 5 * time.Second,
	}
}

func dialLocalContext(ctx context.Context, network, address string) (net.Conn, error) {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return nil, errors.New("invalid local fixture address")
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() || (network != "tcp" && network != "tcp4" && network != "tcp6") {
		return nil, errExternalSocket
	}
	dialer := &net.Dialer{Timeout: 5 * time.Second}
	return dialer.DialContext(ctx, network, address)
}
