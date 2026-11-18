package core

import "strings"

// ParsePermissionCallback validates the shared, prompt-bound permission wire
// format. The bounded ASCII identifier keeps the longest payload below the
// 64-byte callback limit. Legacy unbound buttons must not answer a new prompt.
func ParsePermissionCallback(data string) (callbackID, decision string, ok bool) {
	parts := strings.Split(data, ":")
	if len(parts) != 3 || parts[0] != "perm" || len(parts[1]) == 0 || len(parts[1]) > 48 {
		return "", "", false
	}
	for _, c := range parts[1] {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '_') {
			return "", "", false
		}
	}
	switch parts[2] {
	case "allow", "deny", "allow_all":
		return parts[1], parts[2], true
	default:
		return "", "", false
	}
}
