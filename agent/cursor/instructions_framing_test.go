package cursor

import (
	"os"
	"path/filepath"
	"testing"
)

// These tests only read local instruction files; no CLI or service is invoked.
func TestCursorInstructionsFraming(t *testing.T) {
	const prompt = "  Explain this change.\n"
	tests := []struct {
		name         string
		instructions string
		want         string
	}{
		{
			name:         "project and user sections",
			instructions: "  Follow project rules.\n",
			want:         "Before answering, follow these project-level instructions for this cc-connect turn. They are not user content.\n\nManaged project instructions:\n  Follow project rules.\n\n\n---\n\nUser message:\n" + prompt,
		},
		{
			name:         "empty file preserves prefix",
			instructions: "",
			want:         "\n\n" + prompt,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "AGENTS.md")
			if err := os.WriteFile(path, []byte(tt.instructions), 0600); err != nil {
				t.Fatal(err)
			}
			got, err := prependCursorInstructions(prompt, path)
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Fatalf("prompt = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCursorInstructionsUnsetPreservesPrompt(t *testing.T) {
	for _, prompt := range []string{"", "  User request.\n"} {
		got, err := prependCursorInstructions(prompt, "")
		if err != nil {
			t.Fatal(err)
		}
		if got != prompt {
			t.Fatalf("unset instructions changed prompt: %q, want %q", got, prompt)
		}
	}
}
