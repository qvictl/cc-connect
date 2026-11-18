package feishu

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chenhg5/cc-connect/core"
	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

// Card callbacks authorize only the OpenID namespace: allow_from, admin_from,
// and role UserIDs must use OpenIDs. Text-message UserID/UnionID fallbacks do
// not establish equivalent identities; mixed namespaces may hit default roles
// or wildcard rules. These tests deliberately do not add a callback fallback.
// The pinned Feishu SDK represents user_id as an optional string pointer.
func openIDNamespaceUserID(value string) *string { return &value }

func openIDNamespaceEvent(operator *callback.Operator, action string) *callback.CardActionTriggerEvent {
	return &callback.CardActionTriggerEvent{Event: &callback.CardActionTriggerRequest{
		Operator: operator,
		Action: &callback.CallBackAction{Value: map[string]any{
			"action": action, "session_key": "feishu:oc_test_shared:root:om_test_root",
		}},
		Context: &callback.Context{OpenChatID: "oc_test_shared", OpenMessageID: "om_test_card"},
	}}
}

func TestCardAction_OpenIDRequiredBeforeNavigationHandler(t *testing.T) {
	for _, tc := range []struct {
		name     string
		operator *callback.Operator
	}{
		{"nil-operator", nil},
		{"empty-openid", &callback.Operator{}},
		{"userid-only", &callback.Operator{UserID: openIDNamespaceUserID("ou_test_admin")}},
	} {
		for _, action := range []string{"nav:/provider", "act:/provider b"} {
			t.Run(tc.name+"/"+action, func(t *testing.T) {
				p := &Platform{platformName: "feishu", allowFrom: "*"}
				calls := 0
				p.SetUserAwareCardNavigationHandler(func(string, string, string) *core.Card {
					calls++
					return core.NewCard().Markdown("unexpected handler").Build()
				})
				event := openIDNamespaceEvent(tc.operator, action)
				// A card payload naming an admin session cannot supply a missing actor.
				event.Event.Action.Value["session_key"] = "feishu:oc_test_shared:ou_test_admin"
				resp, err := p.onCardAction(event)
				if err != nil || resp != nil || calls != 0 {
					t.Fatalf("missing OpenID must stop before handler: response=%v err=%v calls=%d", resp, err, calls)
				}
			})
		}
	}
}

func TestCardAction_AllowFromUsesOpenIDNotConflictingUserID(t *testing.T) {
	for _, tc := range []struct {
		name, openID, userID string
		allowed             bool
	}{
		{"member-openid-admin-userid", "ou_test_member", "ou_test_admin", false},
		{"admin-openid-member-userid", "ou_test_admin", "ou_test_member", true},
	} {
		for _, action := range []string{"nav:/provider", "act:/provider b"} {
			t.Run(tc.name+"/"+action, func(t *testing.T) {
				p := &Platform{platformName: "feishu", allowFrom: "ou_test_admin"}
				calls := 0
				var actor string
				p.SetUserAwareCardNavigationHandler(func(_, _, userID string) *core.Card {
					calls++
					actor = userID
					return core.NewCard().Markdown("allowed").Build()
				})
				resp, err := p.onCardAction(openIDNamespaceEvent(&callback.Operator{OpenID: tc.openID, UserID: openIDNamespaceUserID(tc.userID)}, action))
				if err != nil {
					t.Fatal(err)
				}
				if tc.allowed {
					if calls != 1 || actor != tc.openID || resp == nil || resp.Card == nil {
						t.Fatal("allowed OpenID was not forwarded exactly once")
					}
				} else if calls != 0 || resp != nil {
					t.Fatal("UserID bypassed OpenID allow_from")
				}
			})
		}
	}
}

func TestCardAction_RolesAndAdminUseOpenIDNotConflictingUserID(t *testing.T) {
	for _, tc := range []struct {
		name, openID, userID string
		admin               bool
	}{
		{"member-openid-admin-userid", "ou_test_member", "ou_test_admin", false},
		{"admin-openid-member-userid", "ou_test_admin", "ou_test_member", true},
	} {
		for _, action := range []string{"nav:/provider", "act:/provider b", "nav:/dir", "act:/dir select 2"} {
			t.Run(tc.name+"/"+action, func(t *testing.T) {
				// No SDK client, live transport, credential, or agent process.
				p := &Platform{platformName: "feishu", allowFrom: "ou_test_admin,ou_test_member", threadIsolation: true}
				initial, target, dataDir := t.TempDir(), t.TempDir(), t.TempDir()
				agent := &cardClickerAgent{providers: []core.ProviderConfig{{Name: "a"}, {Name: "b"}}, active: "a", workDir: initial}
				e := core.NewEngine("card-test", agent, nil, filepath.Join(dataDir, "sessions.json"), core.LangEnglish)
				t.Cleanup(func() {
					if err := e.Stop(); err != nil {
						t.Errorf("stop callback test engine: %v", err)
					}
				})
				roles := core.NewUserRoleManager()
				roles.Configure("", []core.RoleInput{
					{Name: "admin", UserIDs: []string{"ou_test_admin"}},
					{Name: "member", UserIDs: []string{"ou_test_member"}, DisabledCommands: []string{"provider"}},
				})
				e.SetUserRoles(roles)
				e.SetAdminFrom("ou_test_admin")
				history := core.NewDirHistory(dataDir)
				history.Add("card-test", target)
				history.Add("card-test", initial)
				e.SetDirHistory(history)
				e.OnPlatformReady(p)
				resp, err := p.onCardAction(openIDNamespaceEvent(&callback.Operator{OpenID: tc.openID, UserID: openIDNamespaceUserID(tc.userID)}, action))
				if err != nil || resp == nil || resp.Card == nil {
					t.Fatalf("callback did not return card: response=%v err=%v", resp, err)
				}
				data, err := json.Marshal(resp.Card.Data)
				if err != nil {
					t.Fatal(err)
				}
				denial := "requires admin privilege"
				if strings.Contains(action, "/provider") {
					denial = "Command `/provider` is disabled"
				}
				if !tc.admin {
					if !strings.Contains(string(data), denial) || agent.active != "a" || agent.workDir != initial {
						t.Fatal("conflicting UserID bypassed OpenID role/admin authorization")
					}
				} else {
					if strings.Contains(string(data), denial) {
						t.Fatal("member UserID incorrectly overrode admin OpenID")
					}
					if action == "act:/provider b" && agent.active != "b" {
						t.Fatal("admin OpenID failed provider switch")
					}
					if action == "act:/dir select 2" && agent.workDir != target {
						t.Fatal("admin OpenID failed directory switch")
					}
				}
			})
		}
	}
}
