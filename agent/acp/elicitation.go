package acp

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/chenhg5/cc-connect/core"
)

// elicitationOption is one selectable choice parsed from an elicitation
// form schema. Value is the enum `const` that must round-trip back to the
// agent verbatim; Label is the human-facing title the engine renders and
// echoes back in answers. For kimi acp the two are identical, but the ACP
// schema allows them to differ, so they are tracked separately.
type elicitationOption struct {
	Value       string
	Label       string
	Description string
}

// elicitationQuestion pairs a parsed question with its property key in the
// elicitation form schema (e.g. "q0"), which the response content map is
// keyed by.
type elicitationQuestion struct {
	PropertyKey string
	Required    bool
	Question    core.UserQuestion
	Options     []elicitationOption
}

// elicitationState is cached per pending elicitation/create request so
// RespondPermission can translate engine answers back into the
// CreateElicitationResponse shape the agent expects.
type elicitationState struct {
	Questions []elicitationQuestion
}

// elicitationEnumOption matches the ACP EnumOption shape used by oneOf /
// items.anyOf entries in a form-mode requestedSchema.
type elicitationEnumOption struct {
	Const       string `json:"const"`
	Title       string `json:"title"`
	Description string `json:"description"`
}

type elicitationProperty struct {
	Type        string                  `json:"type"`
	Title       string                  `json:"title"`
	Description string                  `json:"description"`
	OneOf       []elicitationEnumOption `json:"oneOf"`
	Items       *struct {
		AnyOf []elicitationEnumOption `json:"anyOf"`
	} `json:"items"`
}

// parseElicitationCreateParams parses elicitation/create form-mode params
// into ordered questions. Single-select questions arrive as
// type:"string"+oneOf; multi-select as type:"array"+items.anyOf (kimi acp
// also sets minItems:1, making every question required). Property order is
// taken from requestedSchema.required (kimi emits q0, q1, ... in question
// order); without it, keys are sorted for determinism.
//
// Properties that carry no enum options (free-text string/number/boolean
// fields) are skipped only when optional. Required unsupported fields fail
// closed: the option-list wizard cannot produce a complete answer for them.
func parseElicitationCreateParams(params json.RawMessage) (*elicitationState, error) {
	var p struct {
		Mode    string `json:"mode"`
		Message string `json:"message"`
		Schema  struct {
			Properties map[string]elicitationProperty `json:"properties"`
			Required   []string                       `json:"required"`
		} `json:"requestedSchema"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("acp: parse elicitation/create params: %w", err)
	}
	if p.Mode != "" && p.Mode != "form" {
		return nil, fmt.Errorf("acp: unsupported elicitation mode %q", p.Mode)
	}
	if len(p.Schema.Properties) == 0 {
		return nil, fmt.Errorf("acp: elicitation/create with empty requestedSchema.properties")
	}

	required := make(map[string]bool, len(p.Schema.Required))
	for _, key := range p.Schema.Required {
		if _, ok := p.Schema.Properties[key]; !ok {
			return nil, fmt.Errorf("acp: required elicitation property is missing")
		}
		required[key] = true
	}
	order := elicitationPropertyOrder(p.Schema.Properties, p.Schema.Required)
	st := &elicitationState{}
	for _, key := range order {
		prop := p.Schema.Properties[key]
		q, ok := elicitationPropertyToQuestion(prop)
		if !ok {
			if required[key] {
				return nil, fmt.Errorf("acp: required elicitation property cannot be represented")
			}
			continue
		}
		st.Questions = append(st.Questions, elicitationQuestion{
			PropertyKey: key,
			Required:    required[key],
			Question:    q,
			Options:     elicitationEnumOptions(prop),
		})
	}
	if len(st.Questions) == 0 {
		return nil, fmt.Errorf("acp: elicitation/create schema has no enumerable option fields")
	}
	// Core keys answers by question text rather than schema property. If any
	// titles collide, number every title so generated and original titles
	// cannot collide with each other.
	seen := make(map[string]bool, len(st.Questions))
	duplicate := false
	for _, q := range st.Questions {
		duplicate = duplicate || seen[q.Question.Question]
		seen[q.Question.Question] = true
	}
	for i := range st.Questions {
		q := &st.Questions[i]
		if duplicate {
			q.Question.Question = fmt.Sprintf("[%d] %s", i+1, q.Question.Question)
		}
		makeElicitationLabelsRoundTrip(q)
	}
	return st, nil
}

// elicitationPropertyOrder returns property keys in `required` order,
// appending any non-required keys (sorted) afterwards.
func elicitationPropertyOrder(props map[string]elicitationProperty, required []string) []string {
	seen := make(map[string]bool, len(required))
	order := make([]string, 0, len(props))
	for _, key := range required {
		if _, ok := props[key]; ok && !seen[key] {
			seen[key] = true
			order = append(order, key)
		}
	}
	var rest []string
	for key := range props {
		if !seen[key] {
			rest = append(rest, key)
		}
	}
	sort.Strings(rest)
	return append(order, rest...)
}

func elicitationPropertyToQuestion(prop elicitationProperty) (core.UserQuestion, bool) {
	q := core.UserQuestion{Question: prop.Title}
	switch prop.Type {
	case "string":
		if len(prop.OneOf) == 0 {
			return core.UserQuestion{}, false
		}
	case "array":
		if prop.Items == nil || len(prop.Items.AnyOf) == 0 {
			return core.UserQuestion{}, false
		}
		q.MultiSelect = true
	default:
		return core.UserQuestion{}, false
	}
	for _, opt := range elicitationEnumOptions(prop) {
		q.Options = append(q.Options, core.UserQuestionOption{
			Label:       opt.Label,
			Description: opt.Description,
		})
	}
	if q.Question == "" || len(q.Options) == 0 {
		return core.UserQuestion{}, false
	}
	return q, true
}

func elicitationEnumOptions(prop elicitationProperty) []elicitationOption {
	raw := prop.OneOf
	if prop.Type == "array" && prop.Items != nil {
		raw = prop.Items.AnyOf
	}
	opts := make([]elicitationOption, 0, len(raw))
	for _, o := range raw {
		label := o.Title
		if label == "" {
			label = o.Const
		}
		opts = append(opts, elicitationOption{
			Value:       o.Const,
			Label:       label,
			Description: o.Description,
		})
	}
	return opts
}

// Core joins multi-select labels with ", " and trims answers. Use neutral
// ordinal labels only when the original labels are ambiguous in that wire
// format. Keep the full original choice text in the visible description.
func makeElicitationLabelsRoundTrip(q *elicitationQuestion) {
	seen := make(map[string]bool, len(q.Options))
	ambiguous := false
	for _, opt := range q.Options {
		ambiguous = ambiguous || opt.Label == "" || strings.TrimSpace(opt.Label) != opt.Label ||
			seen[opt.Label] || (q.Question.MultiSelect && strings.Contains(opt.Label, ", "))
		seen[opt.Label] = true
	}
	if !ambiguous {
		return
	}
	for i := range q.Options {
		opt := &q.Options[i]
		description := opt.Label
		if opt.Description != "" {
			description += ": " + opt.Description
		}
		opt.Label = fmt.Sprintf("[%d]", i+1)
		q.Question.Options[i].Label = opt.Label
		q.Question.Options[i].Description = description
	}
}

// buildElicitationResponse translates the wizard's text-keyed answers into
// schema property keys and enum values. Required questions must all be answered;
// unknown labels decline the whole form instead of silently accepting a subset.
func buildElicitationResponse(st *elicitationState, result core.PermissionResult) map[string]any {
	decline := map[string]any{"action": "decline"}
	if !strings.EqualFold(result.Behavior, "allow") || st == nil {
		return decline
	}
	answers, ok := result.UpdatedInput["answers"].(map[string]any)
	if !ok || len(answers) == 0 {
		return decline
	}
	content := make(map[string]any, len(st.Questions))
	for _, q := range st.Questions {
		raw, present := answers[q.Question.Question]
		if !present {
			if q.Required {
				return decline
			}
			continue
		}
		ans, ok := raw.(string)
		if !ok || strings.TrimSpace(ans) == "" {
			return decline
		}
		ans = strings.TrimSpace(ans)
		parts := []string{ans}
		if q.Question.MultiSelect {
			parts = strings.Split(ans, ", ")
		}
		selected := make(map[string]bool, len(parts))
		for _, part := range parts {
			matched := false
			for _, opt := range q.Options {
				if part == opt.Label {
					matched = true
					break
				}
			}
			if !matched || selected[part] {
				return decline
			}
			selected[part] = true
		}
		picked := make([]string, 0, len(parts))
		for _, opt := range q.Options {
			if selected[opt.Label] {
				picked = append(picked, opt.Value)
			}
		}
		if q.Question.MultiSelect {
			content[q.PropertyKey] = picked
		} else {
			content[q.PropertyKey] = picked[0]
		}
	}
	if len(content) == 0 {
		return decline
	}
	return map[string]any{"action": "accept", "content": content}
}
