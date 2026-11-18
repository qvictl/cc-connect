//go:build blackbox

package smoke

import "testing"

func TestPermissionActionPreservesPromptBinding(t *testing.T) {
	values := []string{"perm:TEST_ONLY_PROMPT:allow", "perm:TEST_ONLY_PROMPT:deny", "perm:TEST_ONLY_PROMPT:allow_all"}
	for i, decision := range []string{"allow", "deny", "allow_all"} {
		got, err := permissionAction(values, decision)
		if err != nil || got != values[i] {
			t.Fatalf("decision %s: got %q, %v", decision, got, err)
		}
	}
	for _, values := range [][]string{
		nil,
		{"perm:allow"},
		{"perm::allow"},
		{"perm:TEST_ONLY_PROMPT:allow", "perm:TEST_ONLY_OTHER:deny"},
		{"perm:TEST_ONLY_PROMPT:allow", "perm:TEST_ONLY_PROMPT:allow"},
		{"perm:TEST_ONLY_PROMPT:deny"},
	} {
		if _, err := permissionAction(values, "allow"); err == nil {
			t.Fatalf("accepted invalid or mismatched prompt buttons: %q", values)
		}
	}
}
