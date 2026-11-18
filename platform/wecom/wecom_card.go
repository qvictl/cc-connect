package wecom

import (
	"crypto/rand"
	"fmt"
	"io"
	"strings"

	"github.com/chenhg5/cc-connect/core"
)

// buildWeComTemplateCard converts a core.Card into a WeCom template_card payload.
func buildWeComTemplateCard(card *core.Card) (map[string]any, error) {
	if card == nil {
		return nil, fmt.Errorf("wecom: nil card")
	}

	payload := make(map[string]any)

	// Main Title
	if card.Header != nil && card.Header.Title != "" {
		payload["main_title"] = map[string]string{
			"title": card.Header.Title,
		}
	}

	var subTexts []string
	var buttons []map[string]any
	var horizontalList []map[string]string

	for _, elem := range card.Elements {
		switch e := elem.(type) {
		case core.CardMarkdown:
			if strings.TrimSpace(e.Content) != "" {
				subTexts = append(subTexts, e.Content)
			}
		case core.CardNote:
			if strings.TrimSpace(e.Text) != "" {
				subTexts = append(subTexts, e.Text)
			}
		case core.CardListItem:
			item := map[string]string{}
			if e.Text != "" {
				item["keyname"] = e.Text
			}
			if e.BtnText != "" {
				item["value"] = e.BtnText
			}
			if len(item) > 0 {
				horizontalList = append(horizontalList, item)
			}
		case core.CardActions:
			for _, btn := range e.Buttons {
				key, err := weComButtonKey(btn)
				if err != nil {
					return nil, err
				}
				style := 2 // default
				switch btn.Type {
				case "primary":
					style = 1
				case "danger":
					style = 3
				}
				buttons = append(buttons, map[string]any{
					"text":  btn.Text,
					"style": style,
					"key":   key,
				})
			}
		}
	}

	if len(subTexts) > 0 {
		payload["sub_title_text"] = strings.Join(subTexts, "\n\n")
	}

	if len(horizontalList) > 0 {
		payload["horizontal_content_list"] = horizontalList
	}

	// Always add card_action default
	payload["card_action"] = map[string]any{
		"type": 0,
	}

	if len(buttons) > 0 {
		taskID, err := newWeComCardTaskID(rand.Reader)
		if err != nil {
			return nil, err
		}
		payload["card_type"] = "button_interaction"
		payload["button_list"] = buttons
		payload["task_id"] = taskID
		if card.Header == nil || strings.TrimSpace(card.Header.Title) == "" {
			payload["main_title"] = map[string]string{"title": "cc-connect"}
		}
	} else {
		payload["card_type"] = "text_notice"
		// text_notice requires sub_title_text or main_title
		if payload["main_title"] == nil && payload["sub_title_text"] == nil {
			payload["sub_title_text"] = " "
		}
	}

	return payload, nil
}

// newWeComCardTaskID identifies one newly sent interaction card. It is not an
// authentication token or a permission decision binding. The random identifier
// avoids sharing task IDs across concurrent sends and process restarts; its
// ASCII alphabet and 35-byte length fit both WeCom card transports.
func newWeComCardTaskID(random io.Reader) (string, error) {
	var id [16]byte
	if _, err := io.ReadFull(random, id[:]); err != nil {
		return "", fmt.Errorf("wecom: generate card task id: %w", err)
	}
	return fmt.Sprintf("cc_%x", id), nil
}

// weComButtonKey preserves the core prompt binding independently of task_id.
// Core card buttons carry legacy decisions plus the binding in Extra; already
// bound keys are also accepted, but conflicting representations fail closed.
func weComButtonKey(btn core.CardButton) (string, error) {
	if !strings.HasPrefix(strings.TrimSpace(btn.Value), "perm:") {
		return btn.Value, nil
	}
	extra, hasExtra := btn.Extra["perm_request_id"]
	if id, _, ok := core.ParsePermissionCallback(btn.Value); ok {
		if hasExtra && extra != id {
			return "", fmt.Errorf("wecom: conflicting permission button binding")
		}
		return btn.Value, nil
	}
	switch btn.Value {
	case "perm:allow", "perm:deny", "perm:allow_all":
		key := "perm:" + extra + ":" + strings.TrimPrefix(btn.Value, "perm:")
		if _, _, ok := core.ParsePermissionCallback(key); ok {
			return key, nil
		}
	}
	return "", fmt.Errorf("wecom: permission button has missing or invalid binding")
}

// Preserve bound permission events verbatim so core can reject stale prompts.
// Callers must discard a perm: key when this reports false, not treat it as text.
func parseWeComPermissionResponse(eventKey string) (string, bool) {
	_, _, valid := core.ParsePermissionCallback(eventKey)
	return eventKey, valid
}
