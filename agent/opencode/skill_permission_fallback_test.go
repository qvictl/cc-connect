package opencode

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSkillPermission_SiblingDefaultRespectsFileOrder(t *testing.T) {
	for _, fallback := range []string{"deny", "ask"} {
		t.Run(fallback, func(t *testing.T) {
			for order, raw := range []string{
				fmt.Sprintf(`{"*":%q,"skill":{"public-*":"allow"}}`, fallback),
				fmt.Sprintf(`{"skill":{"public-*":"allow"},"*":%q}`, fallback),
			} {
				rules, err := parseOpencodeSkillPermission(json.RawMessage(raw))
				if err != nil {
					t.Fatal(err)
				}
				policy := opencodeSkillPolicy{globalRules: rules}
				if got := policy.decision("private-skill"); got != fallback {
					t.Fatalf("private skill decision = %q, want %q", got, fallback)
				}
				wantPublic := "allow"
				if order == 1 {
					wantPublic = fallback // The later wildcard overrides the earlier skill rule.
				}
				if got := policy.decision("public-example"); got != wantPublic {
					t.Fatalf("public decision = %q, want %q (order %d)", got, wantPublic, order)
				}
			}
		})
	}
}

func TestSkillPermission_MalformedSiblingDefaultFailsClosed(t *testing.T) {
	for _, fallback := range []string{`"invalid"`, `null`, `false`, `[]`, `1`, `{"*":"invalid"}`, `{"*":null}`} {
		for _, skill := range []string{`{"public-*":"allow"}`, `"allow"`} {
			raw := fmt.Sprintf(`{"*":%s,"skill":%s}`, fallback, skill)
			if _, err := parseOpencodeSkillPermission(json.RawMessage(raw)); err == nil {
				t.Fatalf("malformed default accepted: %s", raw)
			}
		}
	}
}

// V1 PermissionRule accepts an action or a pattern/action object. An empty
// object contributes no rules; it is not a malformed action or an implicit deny.
func TestSkillPermission_SiblingPatternObjects(t *testing.T) {
	for _, tc := range []struct {
		raw, public, private string
	}{
		{`{"*":{},"skill":{"public-*":"allow"}}`, "allow", "allow"},
		{`{"skill":{"public-*":"allow"},"*":{}}`, "allow", "allow"},
		{`{"*":{},"skill":"deny"}`, "deny", "deny"},
		{`{"*":{"*":"deny"},"skill":{"public-*":"allow"}}`, "allow", "deny"},
		{`{"skill":{"public-*":"allow"},"*":{"*":"deny"}}`, "deny", "deny"},
	} {
		rules, err := parseOpencodeSkillPermission(json.RawMessage(tc.raw))
		if err != nil {
			t.Fatalf("valid pattern object rejected: %s: %v", tc.raw, err)
		}
		policy := opencodeSkillPolicy{globalRules: rules}
		if got := policy.decision("public-example"); got != tc.public {
			t.Fatalf("%s: public decision = %q, want %q", tc.raw, got, tc.public)
		}
		if got := policy.decision("private-skill"); got != tc.private {
			t.Fatalf("%s: private decision = %q, want %q", tc.raw, got, tc.private)
		}
	}
}

func TestListSkills_SiblingDefaultExcludesPrivatePrompt(t *testing.T) {
	for _, fallback := range []string{"deny", "ask"} {
		for _, scope := range []string{"global", "agent"} {
			t.Run(fallback+"/"+scope, func(t *testing.T) {
				tmp := t.TempDir()
				repo := filepath.Join(tmp, "repo")
				setTestHome(t, filepath.Join(tmp, "home"))
				t.Setenv("OPENCODE_CONFIG", "")
				t.Setenv("OPENCODE_CONFIG_CONTENT", "")
				if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
					t.Fatal(err)
				}
				dir := filepath.Join(repo, ".opencode", "skills")
				writeOpencodeSkill(t, dir, "private-skill", "private-skill", "Private fixture", "PRIVATE_INSTRUCTION_MARKER")
				writeValidOpencodeSkill(t, dir, "public-example")
				permission := fmt.Sprintf(`{"*":%q,"skill":{"public-*":"allow"}}`, fallback)
				config := `{"permission":` + permission + `}`
				a := &Agent{workDir: repo}
				if scope == "agent" {
					config = `{"permission":"allow","agent":{"plan":{"permission":` + permission + `}}}`
					a.agentName = "plan"
				}
				writeOpencodeConfig(t, repo, config)
				skills, err := a.ListSkills(context.Background())
				if err != nil {
					t.Fatal(err)
				}
				if len(skills) != 1 || skills[0].Name != "public-example" {
					t.Fatalf("catalog = %v, want only public-example", skillNames(skills))
				}
				for _, skill := range skills {
					if strings.Contains(skill.Prompt, "PRIVATE_INSTRUCTION_MARKER") {
						t.Fatal("private instructions leaked into catalog")
					}
				}
			})
		}
	}
}
