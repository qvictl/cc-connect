package codex

import (
	"reflect"
	"testing"
)

// Pure argv coverage: no CLI, subprocess, network, or credentials are used.
func TestCodexSubcommandArgs_PreservesConfigInOneScope(t *testing.T) {
	for _, command := range [][]string{
		{"app-server"}, {"exec"}, {"exec", "resume"},
	} {
		t.Run(command[len(command)-1], func(t *testing.T) {
			extra := []string{"--profile", "profile with spaces", "-c", `model_provider="capture"`,
				"--config", `model_providers.capture.base_url="http://127.0.0.1:1/v1"`,
				"--config=model_reasoning_effort=\"medium\"", "-c=model=\"configured\"", "--unknown-option"}
			session := append(append([]string{}, command...), "-c", `model_reasoning_effort="low"`,
				"-c", `model_provider="explicit"`, "-c", `openai_base_url="http://127.0.0.1:2/v1"`)
			extraBefore := append([]string{}, extra...)
			sessionBefore := append([]string{}, session...)
			got, err := codexSubcommandArgs(extra, session)
			if err != nil {
				t.Fatal(err)
			}
			want := []string{"--profile", "profile with spaces", "--unknown-option"}
			want = append(want, command...)
			want = append(want, extra[2:len(extra)-1]...)
			want = append(want, session[len(command):]...)
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("args = %q, want %q", got, want)
			}
			if !reflect.DeepEqual(extra, extraBefore) || !reflect.DeepEqual(session, sessionBefore) {
				t.Fatal("argument inputs were modified")
			}
		})
	}
}

func TestCodexSubcommandArgs_PreservesBoundaryAndProfileValues(t *testing.T) {
	extra := []string{"-p", "-c", "--", "--config", "not-an-override", "-c", "literal"}
	got, err := codexSubcommandArgs(extra, []string{"exec"})
	if err != nil {
		t.Fatal(err)
	}
	want := append(append([]string{}, extra...), "exec")
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("args = %q, want %q", got, want)
	}
}

func TestCodexSubcommandArgs_RejectsMalformedOverrides(t *testing.T) {
	for _, extra := range [][]string{
		{"-c"}, {"--config"}, {"-c", "--profile", "example"},
		{"--config", "missing-equals"}, {"--config="}, {"-c==value"},
	} {
		if _, err := codexSubcommandArgs(extra, []string{"app-server"}); err == nil {
			t.Fatalf("expected malformed config error for %q", extra)
		}
	}
}
