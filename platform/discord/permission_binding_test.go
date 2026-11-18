package discord

import "testing"

func TestPermissionComponentPreservesPromptIdentity(t *testing.T) {
	for _, data := range []string{"perm:prompt-current:allow", "perm:prompt-stale:deny", "perm:prompt-current:allow_all"} {
		got, permission, ok := discordComponentCommand(data)
		if !ok || !permission || got != data { t.Fatal("binding lost") }
	}
	for _, data := range []string{"perm:allow", "perm::allow", "perm:prompt:unknown", "perm:prompt:allow:extra"} {
		if _, _, ok := discordComponentCommand(data); ok { t.Fatal("invalid permission accepted") }
	}
}
