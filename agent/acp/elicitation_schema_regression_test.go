package acp

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/chenhg5/cc-connect/core"
)

func TestElicitationDuplicateTitlesKeepPropertyIdentity(t *testing.T) {
	st, err := parseElicitationCreateParams(json.RawMessage(`{"requestedSchema":{"required":["first","second","third"],"properties":{
		"first":{"type":"string","title":"Choose","oneOf":[{"const":"a","title":"A"}]},
		"second":{"type":"string","title":"Choose","oneOf":[{"const":"b","title":"B"}]},
		"third":{"type":"string","title":"[1] Choose","oneOf":[{"const":"c","title":"C"}]}
	}}}`))
	if err != nil {
		t.Fatal(err)
	}
	answers := map[string]any{}
	for _, q := range st.Questions {
		if _, exists := answers[q.Question.Question]; exists {
			t.Fatalf("duplicate question identity: %q", q.Question.Question)
		}
		answers[q.Question.Question] = q.Question.Options[0].Label
	}
	response := buildElicitationResponse(st, core.PermissionResult{Behavior: "allow", UpdatedInput: map[string]any{"answers": answers}})
	want := map[string]any{"first": "a", "second": "b", "third": "c"}
	if response["action"] != "accept" || !reflect.DeepEqual(response["content"], want) {
		t.Fatalf("response = %#v, want %#v", response, want)
	}
}

func TestElicitationAmbiguousLabelsRoundTripWithoutSplitting(t *testing.T) {
	for _, kind := range []string{"string", "array"} {
		t.Run(kind, func(t *testing.T) {
			options := []map[string]string{
				{"const": "first", "title": "A, B", "description": "combined choice"},
				{"const": "second", "title": "A, B"},
				{"const": "third", "title": " spaced "},
			}
			property := map[string]any{"type": kind, "title": "Pick"}
			if kind == "array" {
				property["items"] = map[string]any{"anyOf": options}
			} else {
				property["oneOf"] = options
			}
			params, err := json.Marshal(map[string]any{"requestedSchema": map[string]any{
				"required": []string{"choice"}, "properties": map[string]any{"choice": property},
			}})
			if err != nil {
				t.Fatal(err)
			}
			st, err := parseElicitationCreateParams(params)
			if err != nil {
				t.Fatal(err)
			}
			q := st.Questions[0]
			if !strings.Contains(q.Question.Options[0].Description, "A, B") || !strings.Contains(q.Question.Options[0].Description, "combined choice") {
				t.Fatal("original choice text was lost")
			}
			answer := q.Question.Options[1].Label
			var want any = "second"
			if kind == "array" {
				answer = q.Question.Options[2].Label + ", " + q.Question.Options[0].Label
				want = []string{"first", "third"}
			}
			response := buildElicitationResponse(st, core.PermissionResult{Behavior: "allow", UpdatedInput: map[string]any{"answers": map[string]any{q.Question.Question: answer}}})
			content, ok := response["content"].(map[string]any)
			if response["action"] != "accept" || !ok || !reflect.DeepEqual(content["choice"], want) {
				t.Fatalf("response = %#v, want choice %#v", response, want)
			}
		})
	}
}

func TestElicitationUnsupportedRequiredFieldFailsClosed(t *testing.T) {
	for _, required := range []string{`["choice","free"]`, `["choice","missing"]`} {
		params := `{"requestedSchema":{"required":` + required + `,"properties":{
			"choice":{"type":"string","title":"Pick","oneOf":[{"const":"yes"}]},
			"free":{"type":"string","title":"Name"}
		}}}`
		if _, err := parseElicitationCreateParams(json.RawMessage(params)); err == nil {
			t.Fatalf("accepted unsupported required field: %s", required)
		}
	}
}

func TestElicitationUniqueCommaLabelRemainsOneSelection(t *testing.T) {
	st, err := parseElicitationCreateParams(json.RawMessage(`{"requestedSchema":{"required":["choice"],"properties":{
		"choice":{"type":"array","title":"Pick","items":{"anyOf":[
			{"const":"combined","title":"A, B"},{"const":"other","title":"C"}
		]}}
	}}}`))
	if err != nil {
		t.Fatal(err)
	}
	q := st.Questions[0]
	response := buildElicitationResponse(st, core.PermissionResult{Behavior: "allow", UpdatedInput: map[string]any{
		"answers": map[string]any{"Pick": q.Question.Options[0].Label},
	}})
	content, ok := response["content"].(map[string]any)
	if !ok || !reflect.DeepEqual(content["choice"], []string{"combined"}) {
		t.Fatalf("comma label split or lost: %#v", response)
	}
}

func TestElicitationRejectsIncompleteAndInvalidAnswers(t *testing.T) {
	st, err := parseElicitationCreateParams(json.RawMessage(elicitationCreateFixture))
	if err != nil {
		t.Fatal(err)
	}
	for name, answers := range map[string]map[string]any{
		"missing required": {"Pick a color": "red"},
		"unknown single": {"Pick a color": "green", "Pick toppings": "cheese"},
		"unknown multi subset": {"Pick a color": "red", "Pick toppings": "cheese, missing"},
		"duplicate selection": {"Pick a color": "red", "Pick toppings": "cheese, cheese"},
		"wrong answer type": {"Pick a color": true, "Pick toppings": "cheese"},
	} {
		response := buildElicitationResponse(st, core.PermissionResult{Behavior: "allow", UpdatedInput: map[string]any{"answers": answers}})
		if response["action"] != "decline" {
			t.Fatalf("%s accepted: %#v", name, response)
		}
	}
}

func TestElicitationOptionalUnsupportedFieldCanBeOmitted(t *testing.T) {
	st, err := parseElicitationCreateParams(json.RawMessage(`{"requestedSchema":{"required":["choice"],"properties":{
		"choice":{"type":"string","title":"Pick","oneOf":[{"const":"yes"}]},
		"free":{"type":"string","title":"Name"}
	}}}`))
	if err != nil {
		t.Fatal(err)
	}
	response := buildElicitationResponse(st, core.PermissionResult{Behavior: "allow", UpdatedInput: map[string]any{"answers": map[string]any{"Pick": "yes"}}})
	if response["action"] != "accept" || len(st.Questions) != 1 {
		t.Fatalf("optional omission rejected: %#v", response)
	}
}
