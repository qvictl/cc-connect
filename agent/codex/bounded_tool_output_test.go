package codex

import (
	"context"
	"strings"
	"testing"

	"github.com/chenhg5/cc-connect/core"
)

func TestAppServerToolReadability_BoundsEveryResultBeforeRedaction(t *testing.T) {
	output := `token="TEST_ONLY_FIXED_HEAD` + strings.Repeat("x\n", 1024*1024) + `TEST_ONLY_FIXED_TAIL"`
	for _, item := range []map[string]any{
		{"type": "commandExecution", "id": "command", "aggregatedOutput": output},
		{"type": "mcpToolCall", "id": "mcp", "result": map[string]any{"content": []any{map[string]any{"text": output}}}},
		{"type": "mcpToolCall", "id": "structured", "result": map[string]any{"structuredContent": map[string]any{"output": output}}},
		{"type": "dynamicToolCall", "id": "dynamic", "contentItems": []any{map[string]any{"type": "inputText", "text": output}}},
		{"type": "webSearch", "id": "search", "query": output},
	} {
		event, ok := appServerToolResultEvent(item)
		if !ok || event.ToolCallID != item["id"] || event.ToolResult != "…" {
			t.Fatalf("unbounded or uncorrelated result: kind=%s bytes=%d", item["type"], len(event.ToolResult))
		}
	}
}

func TestExecToolReadability_BoundsBothResultKindsBeforeRedaction(t *testing.T) {
	output := `token="TEST_ONLY_FIXED_HEAD` + strings.Repeat("x\n", 1024*1024) + `TEST_ONLY_FIXED_TAIL"`
	for _, kind := range []string{"command_execution", "function_call"} {
		s := &codexSession{ctx: context.Background(), events: make(chan core.Event, 1)}
		s.handleItemCompleted(map[string]any{"item": map[string]any{
			"type": kind, "id": "bounded", "status": "completed", "aggregated_output": output, "output": output,
		}})
		event := <-s.events
		if event.ToolResult != "…" || event.ToolCallID != "bounded" {
			t.Fatalf("unbounded or uncorrelated exec result: kind=%s bytes=%d", kind, len(event.ToolResult))
		}
	}
}
