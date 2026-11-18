package core

import (
	"crypto/rand"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestSubagentPreviewRedactsBeforeTruncationInMemory(t *testing.T) {
	// Construct the sensitive-shaped value only at runtime. It never reaches
	// a platform, a log, an error message, or any external service.
	value := rand.Text() + rand.Text() + rand.Text() + rand.Text() + rand.Text() + rand.Text()
	input, err := json.Marshal(map[string]string{"command": "API_KEY=" + value + " run"})
	if err != nil {
		t.Fatal("could not construct input")
	}
	now := time.Now()
	success := false
	exit := 1
	for _, kind := range []ProgressCardEntryKind{ProgressEntryToolUse, ProgressEntryToolResult, ProgressEntryThinking, ProgressEntryInfo} {
		step := ProgressCardEntry{Kind: kind, Tool: "Bash", ToolCallID: "call-one", Input: string(input),
			Text: "begin\nAPI_KEY=" + value + "\nend", Status: "failed", Success: &success, ExitCode: &exit,
			Timestamp: now, ResultTimestamp: now}
		got := subagentStepPreview(step, DisplayCfg{ToolMaxLen: 48, ThinkingMaxLen: 48})
		if strings.Contains(got.Text, value[:16]) || strings.Contains(got.Input, value[:16]) {
			t.Fatal("subagent preview retained sensitive data")
		}
		if !strings.Contains(got.Text, "[redacted]") || !strings.Contains(got.Input, "[redacted]") {
			t.Fatal("redaction was not retained before truncation")
		}
		if !json.Valid([]byte(got.Input)) {
			t.Fatal("structured input lost JSON shape")
		}
		if got.ToolCallID != step.ToolCallID || got.Kind != kind || got.Tool != step.Tool || got.Status != step.Status ||
			got.Success != step.Success || got.ExitCode != step.ExitCode || got.Timestamp != now || got.ResultTimestamp != now {
			t.Fatal("preview changed tool lifecycle metadata")
		}
	}
}
