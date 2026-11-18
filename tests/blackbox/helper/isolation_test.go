package helper

import (
	"flag"
	"path/filepath"
	"strings"
	"testing"
)

func TestBinaryEnvironment_DropsParentCredentialsAndOverrides(t *testing.T) {
	home := filepath.Join("TEST_ONLY", "home")
	requested := make(map[string]bool)
	env := binaryEnvironment(home, func(name string) string {
		requested[name] = true
		switch name {
		case "PATH":
			return "TEST_ONLY_EXECUTABLE_PATH"
		case "SystemRoot", "WINDIR":
			return "TEST_ONLY_OS_ROOT"
		default:
			return "TEST_ONLY_PARENT_SECRET"
		}
	})
	values := make(map[string]string)
	for _, entry := range env {
		name, value, ok := strings.Cut(entry, "=")
		if !ok || strings.Contains(value, "TEST_ONLY_PARENT_SECRET") {
			t.Fatalf("unexpected inherited environment entry: %q", entry)
		}
		if _, exists := values[name]; exists {
			t.Fatalf("duplicate environment key %q", name)
		}
		values[name] = value
	}
	if len(requested) != 3 || !requested["PATH"] || !requested["SystemRoot"] || !requested["WINDIR"] {
		t.Fatalf("parent environment lookups = %v", requested)
	}
	for _, name := range []string{"ANTHROPIC_API_KEY", "OPENAI_API_KEY", "CC_BLACKBOX_CLAUDECODE_API_KEY", "GITHUB_TOKEN", "HTTP_PROXY", "HTTPS_PROXY", "NODE_OPTIONS", "LD_PRELOAD", "GOFLAGS", "SSH_AUTH_SOCK"} {
		if _, exists := values[name]; exists {
			t.Errorf("parent override %s was inherited", name)
		}
	}
	if values["HOME"] != home || values["USERPROFILE"] != home || values["CODEX_HOME"] != filepath.Join(home, ".codex") || values["CLAUDE_CONFIG_DIR"] != filepath.Join(home, ".claude") {
		t.Fatal("agent configuration must stay under the private test home")
	}
	for _, name := range []string{"TMPDIR", "TMP", "TEMP"} {
		if values[name] != filepath.Join(home, "tmp") {
			t.Errorf("%s does not use the private temp directory", name)
		}
	}
}

func TestNeedsLiveProvider_ExplicitBoundary(t *testing.T) {
	for _, tc := range []struct {
		name string
		opts Options
		want bool
	}{
		{"live", Options{}, true},
		{"fake", Options{FakeModel: true}, false},
		{"commands", Options{NoCredentials: true}, false},
		{"fake_commands", Options{FakeModel: true, NoCredentials: true}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := needsLiveProvider(tc.opts); got != tc.want {
				t.Fatalf("needsLiveProvider = %v, want %v", got, tc.want)
			}
		})
	}
	if binaryBuildTags != "no_web,cc_blackbox_loopback" || binaryBridgeToken != "TEST_ONLY_BLACKBOX_BRIDGE_TOKEN" {
		t.Fatal("fixture authentication requires the test-only loopback build")
	}
}

func TestLiveProviders_DefaultOff(t *testing.T) {
	if option := flag.Lookup("blackbox-live"); option == nil || option.DefValue != "false" {
		t.Fatal("live providers must require an explicit test command flag")
	}
}

func TestModuleRoot_AcceptsPortableModuleLines(t *testing.T) {
	for _, text := range []string{
		"module github.com/chenhg5/cc-connect\n",
		"module github.com/chenhg5/cc-connect\r\n",
		"// comment\nmodule\tgithub.com/chenhg5/cc-connect\n",
		"module \"github.com/chenhg5/cc-connect\"\n",
	} {
		if !isCCConnectModule([]byte(text)) {
			t.Fatalf("expected cc-connect module: %q", text)
		}
	}
	if isCCConnectModule([]byte("module example.invalid/other\n")) || isCCConnectModule(nil) {
		t.Fatal("unrelated or empty module must be rejected")
	}
}

func TestBinaryOverride_RejectsEveryPrebuilt(t *testing.T) {
	if err := validateBinaryOverride(""); err != nil {
		t.Fatalf("current-source build rejected: %v", err)
	}
	for _, path := range []string{"TEST_ONLY_OLD_BINARY", "TEST_ONLY_TAGGED_BINARY", " "} {
		if validateBinaryOverride(path) == nil {
			t.Fatal("prebuilt binary must not bypass the loopback source build")
		}
	}
}
