package feishu

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/chenhg5/cc-connect/core"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

// This pure regression exercises the reconciliation without SDK requests.
func TestRefreshQuotedForwardMetadataUsesOuterResourceID(t *testing.T) {
	for _, kind := range []string{"file", "post"} {
		t.Run(kind, func(t *testing.T) {
			body := `{"file_key":"file_TEST_ONLY_FIXED","file_name":"report.txt"}`
			if kind == "post" {
				body = `{"files":[{"file_key":"file_TEST_ONLY_FIXED","file_name":"report.txt"}]}`
			}
			children := map[string][]*larkim.Message{
				"om_nested": {{MessageId: strPtr("om_child"), MsgType: strPtr(kind),
					Sender: &larkim.Sender{Id: strPtr("ou_owner")}, Body: &larkim.MessageBody{Content: &body}}},
			}
			var text strings.Builder
			var images []core.ImageAttachment
			var metadata []quotedFileMeta
			p := &Platform{}
			p.formatMergeForwardTreeWithQuotedFiles("om_nested", "om_outer", children, nil, &text, &images, nil, &metadata, 1)
			if len(metadata) != 1 || metadata[0].messageID != "om_outer" || metadata[0].senderID != "ou_owner" {
				t.Fatal("quoted resource routing or original sender was lost")
			}
		})
	}
}

func TestRefreshTableReplyRetainsFeedbackTail(t *testing.T) {
	content := "| First | Second |\n| --- | --- |\n| one | two |"
	feedback := map[string]any{"tag": "form", "name": "TEST_ONLY_FIXED_FEEDBACK"}
	var card map[string]any
	if err := json.Unmarshal([]byte(buildFinalReplyCardJSON(content, "footer", feedback)), &card); err != nil {
		t.Fatal(err)
	}
	body := card["body"].(map[string]any)["elements"].([]any)
	last := body[len(body)-1].(map[string]any)
	if last["tag"] != "form" || last["name"] != "TEST_ONLY_FIXED_FEEDBACK" {
		t.Fatal("table layout dropped the feedback tail")
	}
	if card["config"].(map[string]any)["width_mode"] != "fill" {
		t.Fatal("full-width card configuration lost")
	}
}
