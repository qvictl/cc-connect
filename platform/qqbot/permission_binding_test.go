package qqbot

import "testing"

func TestPermissionButtonDataPreservesBindingAndScope(t *testing.T) {
	for _, scope := range []string{"qqbot:group:user", "qqbot:g:group", "qqbot:user"} {
		for _, decision := range []string{"allow", "deny", "allow_all"} {
			payload := "perm:prompt-current:" + decision
			got, choice, session, ok := parsePermissionButtonData(payload + ":" + scope)
			if !ok || got != payload || choice != decision || session != scope {
				t.Fatalf("bound callback was not preserved: %q %q %q %v", got, choice, session, ok)
			}
		}
	}
	for _, data := range []string{"perm:allow:qqbot:user", "perm::allow:qqbot:user", "perm:prompt-current:unknown:qqbot:user", "perm:prompt-current:allow:"} {
		if _, _, _, ok := parsePermissionButtonData(data); ok { t.Fatal("accepted unbound or malformed callback") }
	}
}

func TestPermissionCallbackRequiresActualAllowedActorAndChat(t *testing.T) {
	p := &Platform{allowFrom: "alice"}
	if !p.permissionActorMatchesSession(1, "group", "alice", "qqbot:group:alice") { t.Fatal("valid actor rejected") }
	for _, tc := range []struct{ group, actor, scope string }{
		{"group", "bob", "qqbot:group:alice"},
		{"other", "alice", "qqbot:group:alice"},
		{"group", "alice", "qqbot:group:bob"},
	} {
		if p.permissionActorMatchesSession(1, tc.group, tc.actor, tc.scope) { t.Fatal("spoofed scope accepted") }
	}
	p.shareSessionInChannel = true
	if !p.permissionActorMatchesSession(1, "group", "alice", "qqbot:g:group") { t.Fatal("shared scope rejected") }
	if p.permissionActorMatchesSession(1, "group", "bob", "qqbot:g:group") { t.Fatal("shared scope bypassed allowlist") }
	if !p.permissionActorMatchesSession(2, "", "alice", "qqbot:alice") { t.Fatal("direct scope rejected") }
}
