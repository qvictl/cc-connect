package core

import (
	"encoding/json"
	"regexp"
	"strings"
	"unicode/utf8"
)

var (
	toolSecretAssignment = regexp.MustCompile(`(?i)(\b[\w-]*(?:token|secret|password|api[_-]?key|authorization|cookie|credential|bearer|session[_-]?id|access[_-]?key)[\w-]*\s*=\s*)("(?:[^"\\]|\\.)*"|'[^']*'|[^\s"'` + "`" + `&]+)`)
	toolSecretJSON       = regexp.MustCompile(`(?i)("[^"\\]*(?:token|secret|password|api[_-]?key|authorization|cookie|credential|bearer|session[_-]?id|access[_-]?key)[^"\\]*"\s*:\s*)"(?:[^"\\]|\\.)*"`)
	toolSecretHeader     = regexp.MustCompile(`(?i)(Authorization\s*:\s*(?:Bearer|Basic|Token)\s+)("(?:[^"\\]|\\.)*"|'[^']*'|[^\s'"` + "`" + `]+)`)
	toolSecretFlag       = regexp.MustCompile(`(?i)(--[\w-]*(?:token|secret|password|api[_-]?key|credential)[\w-]*\s+)("(?:[^"\\]|\\.)*"|'[^']*'|[^\s]+)`)
	toolURLCredentials   = regexp.MustCompile(`(https?://)[^/\s@]+@`)
)

// MaxToolOutputBytes bounds raw tool output before trimming, JSON parsing, or
// credential matching. Display limits can further shorten the redacted result.
const MaxToolOutputBytes = 32 * 1024

// BoundedToolOutput returns a bounded, redacted preview for adapter events and
// compression output. Oversized output is omitted rather than sliced: a tail
// may start inside a quoted or multiline credential whose key was discarded.
// No raw bytes from an oversized value reach a preview or the redactor.
func BoundedToolOutput(text string) string {
	if len(text) > MaxToolOutputBytes {
		return "…"
	}
	text = RedactToolText(strings.TrimSpace(text))
	// JSON escaping and replacing short values can expand the representation.
	if len(text) > MaxToolOutputBytes {
		return "…"
	}
	return text
}

// RedactToolText removes credentials before display truncation. Callers handling
// unbounded tool output must use BoundedToolOutput before this function.
func RedactToolText(text string) string {
	// Decode before applying shell/header patterns: escaped JSON quotes are not
	// the quoting delimiters of the command stored inside the JSON string.
	if json.Valid([]byte(text)) {
		decoder := json.NewDecoder(strings.NewReader(text))
		decoder.UseNumber()
		var value any
		if decoder.Decode(&value) == nil {
			var changed bool
			value, changed = redactToolValue(value)
			if !changed {
				return text
			}
			var encoded []byte
			var err error
			if strings.Contains(text, "\n") {
				encoded, err = json.MarshalIndent(value, "", "  ")
			} else {
				encoded, err = json.Marshal(value)
			}
			if err == nil {
				return string(encoded)
			}
		}
	}
	return redactPlainToolText(text)
}

var toolSecretName = regexp.MustCompile(`(?i)(token|secret|password|api[_-]?key|authorization|cookie|credential|bearer|session[_-]?id|access[_-]?key)`)

func redactToolValue(value any) (any, bool) {
	changed := false
	switch value := value.(type) {
	case map[string]any:
		for key, field := range value {
			if text, isString := field.(string); isString && toolSecretName.MatchString(key) {
				value[key] = "[redacted]"
				changed = changed || text != "[redacted]"
			} else {
				redacted, fieldChanged := redactToolValue(field)
				value[key] = redacted
				changed = changed || fieldChanged
			}
		}
		return value, changed
	case []any:
		for i, field := range value {
			redacted, fieldChanged := redactToolValue(field)
			value[i] = redacted
			changed = changed || fieldChanged
		}
		return value, changed
	case string:
		// Keep a JSON-looking leaf a string, including when a sibling changes.
		redacted := redactPlainToolText(value)
		return redacted, redacted != value
	default:
		return value, false
	}
}

func redactPlainToolText(text string) string {
	text = toolSecretJSON.ReplaceAllString(text, `${1}"[redacted]"`)
	text = toolSecretHeader.ReplaceAllString(text, `${1}[redacted]`)
	text = toolSecretAssignment.ReplaceAllString(text, `${1}[redacted]`)
	text = toolSecretFlag.ReplaceAllString(text, `${1}[redacted]`)
	return toolURLCredentials.ReplaceAllString(text, `${1}[redacted]@`)
}

// TruncateToolOutput redacts credentials and preserves both the context at the
// start and the actionable result at the end, within the requested rune budget.
func TruncateToolOutput(text string, maxRunes int) string {
	return truncateRedactedToolOutput(RedactToolText(text), maxRunes)
}

// truncateRedactedToolOutput only shortens text. Decoded JSON string leaves
// have already been redacted with their containing document and must not be
// parsed as a second JSON document here.
func truncateRedactedToolOutput(text string, maxRunes int) string {
	if maxRunes > 0 {
		lines := strings.Split(text, "\n")
		if len(lines) > 8 {
			text = strings.Join(lines[:3], "\n") + "\n…\n" + strings.Join(lines[len(lines)-4:], "\n")
		}
	}
	if maxRunes <= 0 || utf8.RuneCountInString(text) <= maxRunes {
		return text
	}
	runes := []rune(text)
	const marker = "\n…\n"
	if maxRunes <= len([]rune(marker)) {
		return string(runes[:maxRunes])
	}
	remaining := maxRunes - len([]rune(marker))
	head := (remaining + 1) / 2
	return string(runes[:head]) + marker + string(runes[len(runes)-(remaining-head):])
}

// toolInputPreview keeps structured input valid until the platform extracts its
// display fields. Cutting a JSON document first loses command/path parameters.
func toolInputPreview(text string, maxRunes int) string {
	text = RedactToolText(text)
	if maxRunes <= 0 || utf8.RuneCountInString(text) <= maxRunes {
		return text
	}
	var value any
	decoder := json.NewDecoder(strings.NewReader(text))
	decoder.UseNumber()
	if !json.Valid([]byte(text)) || decoder.Decode(&value) != nil {
		return TruncateToolOutput(text, maxRunes)
	}
	encoded, err := json.Marshal(truncateToolInputValue(value, maxRunes))
	if err != nil {
		return TruncateToolOutput(text, maxRunes)
	}
	// Leaf limits alone do not bound a document: keys, numbers, containers,
	// escaping and many individually-small leaves all consume the same budget.
	if utf8.RuneCount(encoded) <= maxRunes {
		return string(encoded)
	}
	// Reserve space for keys, scalar values and JSON escaping by reducing
	// the leaf budget, not by replacing an otherwise useful object outright.
	// Each attempt starts from the original decoded value; shortening a
	// shortened leaf would lose its original tail and multiply markers.
	low, high := 3, maxRunes-1
	var fitting []byte
	for low <= high {
		leafBudget := low + (high-low)/2
		candidate, marshalErr := json.Marshal(truncateToolInputValue(value, leafBudget))
		if marshalErr != nil {
			break
		}
		if utf8.RuneCount(candidate) <= maxRunes {
			fitting = candidate
			low = leafBudget + 1
		} else {
			high = leafBudget - 1
		}
	}
	if fitting != nil {
		return string(fitting)
	}
	// Do not slice JSON or expose a prefix of a credential. A JSON string
	// containing an ellipsis is an explicit whole-document omission. Budgets
	// below its three runes cannot represent that marker, so omit the preview.
	if maxRunes < 3 {
		return ""
	}
	return `"…"`
}

// Preserve JSON containers and scalar types while bounding every text leaf.
// toolInputPreview also caps the complete encoded document after this pass.
func truncateToolInputValue(value any, maxRunes int) any {
	switch value := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(value))
		for key, field := range value {
			out[key] = truncateToolInputValue(field, maxRunes)
		}
		return out
	case []any:
		out := make([]any, len(value))
		for i := range value {
			out[i] = truncateToolInputValue(value[i], maxRunes)
		}
		return out
	case string:
		return truncateRedactedToolOutput(value, maxRunes)
	default:
		return value
	}
}

// toolCallID also accepts older adapters that already attach protocol IDs to
// event metadata for tracing.
func toolCallID(event Event) string {
	if event.ToolCallID != "" {
		return event.ToolCallID
	}
	id, _ := event.Metadata["tool_use_id"].(string)
	return strings.TrimSpace(id)
}
