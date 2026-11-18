package antigravity

import (
	"sort"
	"strings"

	"github.com/chenhg5/cc-connect/core"
)

// redactAgyStderr uses the launched process environment, including provider
// overrides. Redact before logging or forwarding stderr to the user.
func redactAgyStderr(text string, env []string) string {
	var values []string
	for _, entry := range env {
		key, value, ok := strings.Cut(entry, "=")
		if !ok || value == "" {
			continue
		}
		key = strings.ToUpper(key)
		for _, sensitive := range []string{"KEY", "TOKEN", "SECRET", "PASSWORD", "PASSWD", "CREDENTIAL", "AUTH", "COOKIE"} {
			if strings.Contains(key, sensitive) {
				values = append(values, value)
				break
			}
		}
	}
	// Replace longer overlapping values first, in one pass, so neither a
	// shorter prefix nor a replacement marker exposes part of another value.
	sort.Slice(values, func(i, j int) bool { return len(values[i]) > len(values[j]) })
	pairs := make([]string, 0, len(values)*2)
	for _, value := range values {
		pairs = append(pairs, value, "[redacted]")
	}
	if len(pairs) > 0 {
		text = strings.NewReplacer(pairs...).Replace(text)
	}
	return strings.TrimSpace(core.RedactToolText(text))
}
