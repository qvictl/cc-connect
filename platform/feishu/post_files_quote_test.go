package feishu

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/chenhg5/cc-connect/core"
	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

func TestQuotedPostFilesStayLazyAndRetainSender(t *testing.T) {
	var downloads atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(r.URL.Path, "tenant_access_token"):
			writeJSON(t, w, map[string]any{"code": 0, "tenant_access_token": fixedTestTenantToken, "expire": 7200})
		case strings.Contains(r.URL.Path, "/resources/"):
			downloads.Add(1)
			t.Error("quoted metadata lookup eagerly downloaded a file")
		case strings.Contains(r.URL.Path, "/contact/"):
			writeJSON(t, w, map[string]any{"code": 0, "data": map[string]any{"user": map[string]any{"name": "Owner"}}})
		default:
			writeJSON(t, w, map[string]any{"code": 0, "data": map[string]any{"items": []any{map[string]any{"msg_type": "post", "sender": map[string]any{"id": "ou_owner", "sender_type": "user"}, "body": map[string]any{"content": `{"files":[{"file_key":"file_a","file_name":"a.csv"}]}`}}}}})
		}
	}))
	t.Cleanup(srv.Close)
	p := &Platform{platformName: "feishu", domain: srv.URL, botOpenID: "ou_bot", client: lark.NewClient("lazy_post_files", fixedTestAppSecret, lark.WithEnableTokenCache(false), lark.WithOpenBaseUrl(srv.URL), lark.WithHttpClient(fixtureHTTPClient(t, srv.URL)))}
	msg := p.fetchSingleMessage(context.Background(), "om_parent")
	if msg == nil || len(msg.files) != 1 {
		t.Fatal("files-only quoted post was dropped")
	}
	f := msg.files[0]
	if f.messageID != "om_parent" || f.senderID != "ou_owner" || f.fileKey != "file_a" {
		t.Fatal("quoted file identity lost")
	}
	if downloads.Load() != 0 {
		t.Fatal("quoted files fetched eagerly")
	}
	mentions := []*larkim.MentionEvent{{Id: &larkim.UserId{OpenId: strPtr("ou_bot")}}}
	if len(p.filterQuotedFilesForUser(msg.files, nil, "ou_owner")) != 0 {
		t.Fatal("missing mention allowed")
	}
	if len(p.filterQuotedFilesForUser(msg.files, mentions, "ou_other")) != 0 {
		t.Fatal("foreign sender allowed")
	}
	if len(p.filterQuotedFilesForUser(msg.files, mentions, "ou_owner")) != 1 {
		t.Fatal("same sender mention rejected")
	}
}

// Quoting nested forwarded files must preserve the child sender and defer every
// file-resource request until the normal mention/same-sender gate has passed.
func TestQuotedForwardFilesWaitForMentionAndSender(t *testing.T) {
	for _, msgType := range []string{"post", "file"} {
		for _, tc := range []struct {
			name    string
			mention bool
			userID  string
			want    int32
		}{
			{"no mention", false, "ou_owner", 0},
			{"foreign sender", true, "ou_other", 0},
			{"same sender", true, "ou_owner", 1},
		} {
			t.Run(msgType+"/"+tc.name, func(t *testing.T) {
				var downloads atomic.Int32
				body := `{"file_key":"file_owned","file_name":"owned.csv"}`
				if msgType == "post" {
					body = `{"files":[{"file_key":"file_owned","file_name":"owned.csv"}]}`
				}
				item := func(id, parent, kind, sender, content string) map[string]any {
					return map[string]any{"message_id": id, "upper_message_id": parent, "msg_type": kind,
						"sender": map[string]any{"id": sender, "sender_type": "user"}, "body": map[string]any{"content": content}}
				}
				items := []any{
					item("om_root", "", "merge_forward", "ou_forwarder", ""),
					item("om_nested", "om_root", "merge_forward", "ou_forwarder", ""),
					item("om_owned", "om_nested", msgType, "ou_owner", body),
					item("om_foreign", "om_nested", msgType, "ou_foreign", strings.ReplaceAll(body, "file_owned", "file_foreign")),
				}
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					switch {
					case strings.Contains(r.URL.Path, "tenant_access_token"):
						writeJSON(t, w, map[string]any{"code": 0, "tenant_access_token": fixedTestTenantToken, "expire": 7200})
					case strings.Contains(r.URL.Path, "/resources/"):
						downloads.Add(1)
						if tc.want == 0 || r.URL.Path != "/open-apis/im/v1/messages/om_root/resources/file_owned" {
							t.Error("quoted forward requested an unapproved file")
						}
						w.Header().Set("Content-Type", "application/octet-stream")
						_, _ = w.Write([]byte("column\nvalue\n"))
					case strings.Contains(r.URL.Path, "/contact/"):
						writeJSON(t, w, map[string]any{"code": 0, "data": map[string]any{"user": map[string]any{"name": "Owner"}}})
					default:
						writeJSON(t, w, map[string]any{"code": 0, "data": map[string]any{"items": items}})
					}
				}))
				t.Cleanup(srv.Close)
				var got *core.Message
				p := &Platform{
					platformName: "feishu", domain: srv.URL, botOpenID: "ou_bot",
					client: lark.NewClient("quoted_forward_files", fixedTestAppSecret,
						lark.WithEnableTokenCache(false), lark.WithOpenBaseUrl(srv.URL), lark.WithHttpClient(fixtureHTTPClient(t, srv.URL))),
					resourceDownloadHTTP: fixtureHTTPClient(t, srv.URL),
					fetchResourceToken:   func(context.Context) (string, error) { return fixedTestTenantToken, nil },
					handler:              func(_ core.Platform, msg *core.Message) { got = msg },
				}
				var mentions []*larkim.MentionEvent
				if tc.mention {
					mentions = []*larkim.MentionEvent{{Id: &larkim.UserId{OpenId: strPtr("ou_bot")}}}
				}
				p.dispatchMessage(context.Background(), "text", `{"text":"analyze"}`, mentions,
					"om_reply", "session", tc.userID, "", replyContext{}, "om_root", 0)
				if got == nil || int32(len(got.Files)) != tc.want {
					t.Fatal("quoted forward did not preserve the gated file delivery")
				}
				if downloads.Load() != tc.want {
					t.Fatalf("file downloads = %d, want %d", downloads.Load(), tc.want)
				}
			})
		}
	}
}
