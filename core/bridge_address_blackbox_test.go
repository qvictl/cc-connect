//go:build cc_blackbox_loopback

package core

import (
	"net"
	"testing"
)

func TestBridgeListenAddress_BlackboxIsLoopback(t *testing.T) {
	addr := bridgeListenAddress(9810)
	host, port, err := net.SplitHostPort(addr)
	if err != nil || host != "127.0.0.1" || port != "9810" {
		t.Fatalf("test bridge address = %q, want 127.0.0.1:9810", addr)
	}
	if ip := net.ParseIP(host); ip == nil || !ip.IsLoopback() {
		t.Fatal("test bridge must bind loopback before fixture authentication is used")
	}
}
