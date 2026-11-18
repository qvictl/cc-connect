//go:build cc_blackbox_loopback

package core

import "fmt"

// cc_blackbox_loopback is reserved for test-instrumented binaries. Never
// publish these as release artifacts: fixture authentication is predictable.
func bridgeListenAddress(port int) string {
	return fmt.Sprintf("127.0.0.1:%d", port)
}
