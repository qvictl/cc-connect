package pi

import (
	"encoding/json"
	"fmt"
	"testing"
)

func TestThinkingLevels_ExtendedMappingsRequireStrings(t *testing.T) {
	for _, value := range []any{nil, false, true, 3, map[string]any{}, []any{}} {
		m := &piModel{Reasoning: true, ThinkingLevelMap: map[string]any{"xhigh": value, "max": value}}
		if got := fmt.Sprint(thinkingLevelsForDef(m)); got != "[off minimal low medium high]" {
			t.Fatalf("non-string mapping of type %T enabled extended levels: %s", value, got)
		}
	}
	m := &piModel{Reasoning: true, ThinkingLevelMap: map[string]any{"xhigh": "", "max": "maximum", "low": nil}}
	if got := fmt.Sprint(thinkingLevelsForDef(m)); got != "[off minimal medium high xhigh max]" {
		t.Fatalf("string mappings or explicit disabled level lost: %s", got)
	}
}

func TestModelCatalog_PartialCustomMetadataPreservesCapabilities(t *testing.T) {
	for _, test := range []struct {
		name   string
		fields map[string]any
		want   string
	}{
		{"omitted", map[string]any{"name": "Custom display", "contextWindow": 64000}, "[off minimal low medium high max]"},
		{"explicit_false", map[string]any{"reasoning": false}, "[off]"},
		{"explicit_null_reasoning", map[string]any{"reasoning": nil}, "[off]"},
		{"explicit_null_map", map[string]any{"thinkingLevelMap": nil}, "[off minimal low medium high]"},
		{"explicit_empty_map", map[string]any{"thinkingLevelMap": map[string]any{}}, "[off minimal low medium high]"},
		{"explicit_map", map[string]any{"thinkingLevelMap": map[string]any{"max": nil, "xhigh": "extra"}}, "[off minimal low medium high xhigh]"},
	} {
		t.Run(test.name, func(t *testing.T) {
			custom := map[string]any{"id": "reasoner"}
			for name, value := range test.fields {
				custom[name] = value
			}
			writePiModelFiles(t, nil,
				map[string]any{"providers": map[string]any{"p": map[string]any{"models": []any{custom}}}},
				map[string]any{"p": map[string]any{"models": []any{
					map[string]any{"id": "reasoner", "name": "Bundled", "contextWindow": 32000, "reasoning": true, "thinkingLevelMap": map[string]any{"max": "max"}},
				}}},
			)
			_, model, ok := lookupModelDef("p/reasoner")
			if !ok {
				t.Fatal("custom model missing")
			}
			if got := fmt.Sprint(thinkingLevelsForDef(model)); got != test.want {
				t.Fatalf("efforts = %s, want %s", got, test.want)
			}
			if test.name == "omitted" && (model.Name != "Custom display" || model.ContextWindow != 64000) {
				t.Fatal("explicit display/context overrides lost")
			}
		})
	}
}

func TestPiModelCapabilityPresence_MatchesJSONFieldNames(t *testing.T) {
	var model piModel
	if err := json.Unmarshal([]byte(`{"id":"m","REASONING":false,"THINKINGLEVELMAP":null}`), &model); err != nil {
		t.Fatal(err)
	}
	if !model.reasoningPresent || !model.thinkingMapPresent {
		t.Fatal("case-insensitive explicit overrides treated as missing")
	}
	if err := json.Unmarshal([]byte(`{"id":"new"}`), &model); err != nil {
		t.Fatal(err)
	}
	if model.reasoningPresent || model.thinkingMapPresent {
		t.Fatal("old presence retained when decoding a new definition")
	}
}

func TestModelCatalog_NewCustomModelDoesNotInventCapabilities(t *testing.T) {
	writePiModelFiles(t, nil,
		map[string]any{"providers": map[string]any{"p": map[string]any{"models": []any{
			map[string]any{"id": "new"},
		}}}}, nil,
	)
	_, model, ok := lookupModelDef("p/new")
	if !ok || fmt.Sprint(thinkingLevelsForDef(model)) != "[off]" {
		t.Fatal("custom model without a base acquired reasoning capabilities")
	}
}
