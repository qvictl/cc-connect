package telegram

import (
	"strings"
	"testing"
)

func TestPermissionCallbackKeepsBindingWithinTelegramLimit(t *testing.T) {
	for _, decision := range []string{"allow", "deny", "allow_all"} {
		for _, id := range []string{"prompt-current", "prompt-stale", strings.Repeat("A", 48)} {
			data := "perm:" + id + ":" + decision
			got, ok := permissionCallbackContent(data)
			if !ok || got != data || len(data) > 64 { t.Fatal("binding lost or callback exceeds Telegram limit") }
		}
	}
	for _, data := range []string{"perm:allow", "perm::deny", "perm:prompt:unknown", "perm:prompt:allow:extra"} {
		if _, ok := permissionCallbackContent(data); ok { t.Fatal("legacy or malformed callback accepted") }
	}
}
