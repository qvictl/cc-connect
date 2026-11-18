//go:build !cc_blackbox_loopback

package core

import "fmt"

// Ordinary builds retain the existing bridge listen address.
func bridgeListenAddress(port int) string {
	return fmt.Sprintf(":%d", port)
}
