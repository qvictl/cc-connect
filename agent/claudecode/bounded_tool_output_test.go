package claudecode

import (
	"context"
	"strings"
	"testing"

	"github.com/chenhg5/cc-connect/core"
)

func TestClaudeToolReadability_BoundsOutputBeforeRedaction(t *testing.T) {
	output := `token="TEST_ONLY_FIXED_HEAD` + strings.Repeat("x\n", 1024*1024) + `TEST_ONLY_FIXED_TAIL"`
	for _, content := range []any{output, []any{map[string]any{"type": "text", "text": output}}} {
		s := &claudeSession{ctx: context.Background(), events: make(chan core.Event, 1)}
		s.handleUser(map[string]any{"message": map[string]any{"content": []any{
			map[string]any{"type": "tool_result", "tool_use_id": "bounded", "content": content, "is_error": true},
		}}})
		event := <-s.events
		if event.ToolResult != "…" || event.ToolCallID != "bounded" || event.ToolSuccess == nil || *event.ToolSuccess {
			t.Fatalf("unbounded or misleading Claude result: bytes=%d", len(event.ToolResult))
		}
	}
}
