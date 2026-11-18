package core

import (
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestToolInputPreview_TotalJSONBudget(t *testing.T) {
	for name, input := range map[string]string{
		"many small leaves": `[` + strings.Repeat(`"small",`, 40) + `"end"]`,
		"numbers":           `[` + strings.Repeat(`1234567890,`, 40) + `0]`,
		"containers":        strings.Repeat(`[`, 40) + `0` + strings.Repeat(`]`, 40),
		"long key":          `{"` + strings.Repeat("界", 80) + `":1}`,
		"escaped leaf":      `{"value":"` + strings.Repeat(`\u0001`, 80) + `"}`,
		"secret":            `{"token":"TEST_ONLY_FIXED_SECRET","padding":"` + strings.Repeat("x", 100) + `"}`,
	} {
		t.Run(name, func(t *testing.T) {
			for _, budget := range []int{1, 2, 3, 10, 32} {
				got := toolInputPreview(input, budget)
				if utf8.RuneCountInString(got) > budget || !utf8.ValidString(got) {
					t.Fatalf("preview exceeds total rune budget %d", budget)
				}
				if budget < 3 {
					if got != "" {
						t.Fatalf("tiny budget %d must omit preview", budget)
					}
				} else if !json.Valid([]byte(got)) {
					t.Fatalf("budget %d produced invalid JSON", budget)
				}
				if strings.Contains(got, "TEST_ONLY_FIXED_SECRET") {
					t.Fatal("secret survived preview")
				}
			}
		})
	}
}

func TestToolInputPreview_FittingJSONPreservesBytesAndTypes(t *testing.T) {
	input := "{\n  \"number\": 9007199254740993, \"text\": \"界\"\n}"
	if got := toolInputPreview(input, utf8.RuneCountInString(input)); got != input {
		t.Fatal("fitting unchanged JSON lost byte fidelity")
	}
	if got := toolInputPreview(input, 0); got != input {
		t.Fatal("unlimited preview changed")
	}
	input = `{"value":"` + strings.Repeat("x", 100) + `"}`
	got := toolInputPreview(input, 32)
	var object map[string]string
	if err := json.Unmarshal([]byte(got), &object); err != nil || object["value"] == "" || !strings.Contains(object["value"], "…") || utf8.RuneCountInString(got) > 32 {
		t.Fatalf("fitting structure must survive within total budget, got %q", got)
	}
}

func TestRedactToolValue_DecodedStringIsNotReparsed(t *testing.T) {
	// The inner JSON is a string, not another document. A sibling forces
	// serialization, but must not rewrite whitespace/escapes in that string.
	inner := "{\n  \"number\": \"42\", \"value\": \"<\"\n}"
	input, err := json.Marshal(map[string]any{"token": "TEST_ONLY_FIXED_SECRET", "nested": inner})
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(RedactToolText(string(input))), &got); err != nil {
		t.Fatal(err)
	}
	if got["nested"] != inner || got["token"] != "[redacted]" {
		t.Fatal("decoded string or secret changed unexpectedly")
	}
}

func TestRedactToolValue_JSONLookingStringRedactsWithoutReserializing(t *testing.T) {
	inner := `{  "token" : "TEST_ONLY_FIXED_NESTED", "ordinary" : "42" }`
	input, err := json.Marshal(map[string]any{"nested": inner})
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(RedactToolText(string(input))), &got); err != nil {
		t.Fatal(err)
	}
	if got["nested"] != `{  "token" : "[redacted]", "ordinary" : "42" }` {
		t.Fatal("JSON-looking string was recursively reserialized")
	}
}

func TestTruncateToolInputValue_DoesNotReinterpretDecodedStrings(t *testing.T) {
	// This helper's input has already passed document-level redaction. Pin
	// its shortening-only contract independently: JSON-looking string data
	// must not trigger another parse/redaction pass.
	value := `{  "token" : "TEST_ONLY_FIXED_HELPER_VALUE", "n" : "42" }`
	got := truncateToolInputValue(value, utf8.RuneCountInString(value))
	if got != value {
		t.Fatal("decoded leaf was reinterpreted instead of only shortened")
	}
	short := truncateToolInputValue(value, 12)
	text, ok := short.(string)
	if !ok || utf8.RuneCountInString(text) > 12 || !utf8.ValidString(text) {
		t.Fatal("decoded leaf shortening lost string type or rune bound")
	}
}
