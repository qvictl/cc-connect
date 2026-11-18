package core

import (
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestToolInputPreview_StructuredBudgetRetainsTailAndTypes(t *testing.T) {
	for _, leaf := range []string{
		"HEAD" + strings.Repeat("界", 1000) + "TAIL",
		"HEAD" + strings.Repeat("\x01", 1000) + "TAIL",
	} {
		original := map[string]any{"nested": []any{map[string]any{"query": leaf}}, "id": json.Number("9007199254740993"), "enabled": true}
		input, err := json.Marshal(original)
		if err != nil { t.Fatal(err) }
		got := toolInputPreview(string(input), 160)
		var decoded struct {
			Nested []struct { Query string `json:"query"` } `json:"nested"`
			ID json.Number `json:"id"`
			Enabled bool `json:"enabled"`
		}
		if err := json.Unmarshal([]byte(got), &decoded); err != nil { t.Fatal(err) }
		if utf8.RuneCountInString(got) > 160 || len(decoded.Nested) != 1 || decoded.ID.String() != "9007199254740993" || !decoded.Enabled {
			t.Fatalf("budget or scalar types changed: %q", got)
		}
		query := decoded.Nested[0].Query
		if !strings.HasPrefix(query, "HEAD") || !strings.HasSuffix(query, "TAIL") || strings.Count(query, "…") != 1 {
			t.Fatalf("adaptive shortening lost original head/tail: %q", query)
		}
		_ = truncateToolInputValue(original, 10)
		if original["nested"].([]any)[0].(map[string]any)["query"] != leaf {
			t.Fatal("shortening mutated the source tree used by subsequent attempts")
		}
	}
}

func TestToolInputPreview_UnfittableStructureIsExplicitlyOmitted(t *testing.T) {
	input := `{"request_id":9007199254740993,"command":"` + strings.Repeat("x", 100) + `"}`
	// Even empty command text cannot make the fixed keys and exact ID fit.
	if got := toolInputPreview(input, 40); got != `"…"` {
		t.Fatalf("unfittable structure must not corrupt numbers or exceed budget: %q", got)
	}
}
