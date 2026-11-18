//go:build !cc_blackbox_loopback

package core

import "testing"

func TestBridgeListenAddress_PreservesProductionDefault(t *testing.T) {
	if got := bridgeListenAddress(9810); got != ":9810" {
		t.Fatalf("production bridge address = %q, want :9810", got)
	}
}
