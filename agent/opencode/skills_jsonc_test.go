package opencode

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestParseOpencodeJSONC_TrailingCommasPreserveStrings(t *testing.T) {
	raw := `{
		// A comma after a comment is still a trailing separator.
		"paths": ["one", "two", /* keep boundary */],
		"literal": "https://example.invalid/a/*literal*/,]}",
		"escaped": "quote: \" and slash: \\",
		"nested": {"enabled": true,},
	}`
	got, err := parseOpencodeJSONC(raw)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(got, &decoded); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{
		"paths":   []any{"one", "two"},
		"literal": "https://example.invalid/a/*literal*/,]}",
		"escaped": "quote: \" and slash: \\",
		"nested":  map[string]any{"enabled": true},
	}
	if !reflect.DeepEqual(decoded, want) {
		t.Fatalf("decoded = %#v, want %#v", decoded, want)
	}
}

func TestParseOpencodeJSONC_MalformedInputStillRejected(t *testing.T) {
	for _, raw := range []string{
		`{"value": 1/* comment */2}`,
		`{"value": tr/* comment */ue}`,
		"{\"value\": 1// comment\n2}",
		`{} /* unterminated`,
		`{} /*`,
		`{} /**`,
		`{,}`,
		`[,]`,
		`{"value":,}`,
		`[1,,]`,
		`{"value": 1,,}`,
		`{"value": "unterminated}`,
	} {
		if _, err := parseOpencodeJSONC(raw); err == nil {
			t.Errorf("invalid JSONC accepted: %q", raw)
		}
	}
}

func TestParseOpencodeJSONC_PermissionFallbackSurvivesTrailingComma(t *testing.T) {
	raw, err := parseOpencodeJSONC(`{
		"permission": {
			"*": "deny", // preserve the default for unmatched skills
			"skill": {"public-*": "allow",},
		},
	}`)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := parseOpencodeConfig(raw)
	if err != nil {
		t.Fatal(err)
	}
	policy := opencodeSkillPolicy{globalRules: cfg.globalRules}
	if got := policy.decision("private-skill"); got != "deny" {
		t.Fatalf("private skill decision = %q, want deny", got)
	}
	if got := policy.decision("public-example"); got != "allow" {
		t.Fatalf("public skill decision = %q, want allow", got)
	}
}
