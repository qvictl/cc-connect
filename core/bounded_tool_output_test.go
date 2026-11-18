package core

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestBoundedToolOutput_OmitsOversizedRawInputBeforeRedaction(t *testing.T) {
	for name, input := range map[string]string{
		"plain":       "START\n" + strings.Repeat("build output\n", 200000) + "FAIL",
		"single line": strings.Repeat("x", 2*1024*1024),
		"JSON":        `{"output":"` + strings.Repeat("x", 2*1024*1024) + `"}`,
		"whitespace":  strings.Repeat(" ", 2*1024*1024),
		"assignment":  `token="TEST_ONLY_FIXED_HEAD` + strings.Repeat("x\n", MaxToolOutputBytes) + `TEST_ONLY_FIXED_TAIL"`,
		"JSON secret": `{"token":"TEST_ONLY_FIXED_HEAD` + strings.Repeat(`\n`, MaxToolOutputBytes) + `TEST_ONLY_FIXED_TAIL"}`,
		"URL":         "https://user:TEST_ONLY_FIXED_HEAD" + strings.Repeat("x", MaxToolOutputBytes) + "TEST_ONLY_FIXED_TAIL@example.invalid/",
	} {
		t.Run(name, func(t *testing.T) {
			if got := BoundedToolOutput(input); got != "…" {
				t.Fatalf("oversized raw output was retained: bytes=%d", len(got))
			}
		})
	}
}

func TestBoundedToolOutput_ByteBoundaryAndRedactionExpansion(t *testing.T) {
	for _, input := range []string{
		strings.Repeat("x", MaxToolOutputBytes),
		strings.Repeat("界", MaxToolOutputBytes/3),
		"START\n" + strings.Repeat("building\n", 100) + "FAIL",
	} {
		if got := BoundedToolOutput(input); got != input {
			t.Fatalf("bounded ordinary output changed: bytes=%d", len(got))
		}
	}
	if BoundedToolOutput(strings.Repeat("x", MaxToolOutputBytes+1)) != "…" {
		t.Fatal("raw byte cap exceeded")
	}
	input := `[` + strings.Repeat(`{"token":""},`, 1800) + `{}]`
	if len(input) > MaxToolOutputBytes || len(RedactToolText(input)) <= MaxToolOutputBytes {
		t.Fatal("fixture does not exercise redaction expansion")
	}
	if got := BoundedToolOutput(input); got != "…" {
		t.Fatalf("redacted representation exceeded cap: bytes=%d", len(got))
	}
	if got := BoundedToolOutput("  token=TEST_ONLY_FIXED_VALUE  "); strings.Contains(got, "TEST_ONLY_FIXED_VALUE") || !strings.Contains(got, "[redacted]") {
		t.Fatal("ordinary output was not redacted")
	}
}

func TestRedactToolText_PreservesUnchangedJSONBytesAndStringTypes(t *testing.T) {
	for _, input := range []string{
		"{\n\t\"z\": \"42\",\n\t\"a\": [\"true\", \"null\", \"[1, 2]\"]\n}\n",
		`{"nested":"{\"z\":2, \"a\":1}","escaped":"\u003c"}`,
		`"42"`,
	} {
		if got := RedactToolText(input); got != input {
			t.Fatal("unchanged JSON was reserialized")
		}
	}
	input := `{"token":"TEST_ONLY_FIXED_VALUE","number":"42","boolean":"true","null":"null","nested":"{\"z\":2, \"a\":1}","id":9007199254740993}`
	var got map[string]any
	decoder := json.NewDecoder(strings.NewReader(RedactToolText(input)))
	decoder.UseNumber()
	if err := decoder.Decode(&got); err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]string{"token": "[redacted]", "number": "42", "boolean": "true", "null": "null", "nested": `{"z":2, "a":1}`} {
		if value, ok := got[key].(string); !ok || value != want {
			t.Fatalf("string leaf changed: %s", key)
		}
	}
	if got["id"] != json.Number("9007199254740993") {
		t.Fatal("numeric precision changed")
	}
}

func TestToolReadability_LegacyPairingRecoversOnlyAfterAllPendingCallsDrain(t *testing.T) {
	for _, native := range []bool{false, true} {
		p := &previewCapturePlatform{}
		w := newCompactProgressWriter(context.Background(), p, progressHintReplyCtx{style: "card", payload: true}, "test", LangEnglish, nil)
		appendItem := func(item ProgressCardEntry) { w.AppendStructured(item, item.Text) }
		first := ProgressCardEntry{Kind: ProgressEntryToolUse, Tool: "first", Text: "old first"}
		if native {
			first.ToolCallID = "native"
		}
		appendItem(first)
		appendItem(ProgressCardEntry{Kind: ProgressEntryToolUse, Tool: "second", Text: "old second"})
		appendItem(ProgressCardEntry{Kind: ProgressEntryToolResult, Text: "unknown"})
		if !w.legacyUncertain {
			t.Fatal("ambiguous result was not tracked")
		}
		appendItem(ProgressCardEntry{Kind: ProgressEntryToolResult, Tool: "first", ToolCallID: first.ToolCallID, Text: "first done"})
		if !w.legacyUncertain {
			t.Fatal("ambiguity cleared while a legacy call was pending")
		}
		appendItem(ProgressCardEntry{Kind: ProgressEntryToolResult, Tool: "second", Text: "second done"})
		if w.legacyUncertain || len(w.pendingTools) != 0 || len(w.legacyTools) != 0 {
			t.Fatal("ambiguity survived a fully drained pending set")
		}
		appendItem(ProgressCardEntry{Kind: ProgressEntryToolUse, Tool: "fresh", Text: "fresh input"})
		appendItem(ProgressCardEntry{Kind: ProgressEntryToolResult, Tool: "fresh", Text: "fresh result"})
		last := w.items[len(w.items)-1]
		if last.Input != "fresh input" || last.Text != "fresh result" {
			t.Fatal("fresh legacy pair did not recover")
		}
	}
}

func TestProcessCompressEvents_BoundsAndRedactsBothToolResultFields(t *testing.T) {
	for _, field := range []string{"content", "tool_result"} {
		for _, oversized := range []bool{false, true} {
			e := newTestEngine()
			t.Cleanup(func() { _ = e.Stop() })
			p := &stubPlatformEngine{n: "test"}
			s := newQueuingSession("bounded-compress")
			state := &interactiveState{agentSession: s, platform: p}
			const key = "test:bounded-compress"
			session := e.sessions.GetOrCreateActive(key)
			lockGen, _ := session.TryLock()
			input := "token=TEST_ONLY_FIXED_VALUE"
			if oversized {
				input += strings.Repeat("x", 2*1024*1024)
			}
			event := Event{Type: EventToolResult, ToolName: "shell"}
			if field == "content" {
				event.Content = input
			} else {
				event.ToolResult = input
			}
			s.events <- event
			s.events <- Event{Type: EventResult, Done: true}
			unlocked := false
			e.processCompressEvents(state, session, e.sessions, key, p, nil, &unlocked, false, lockGen)
			session.Unlock(lockGen)
			sent := strings.Join(p.getSent(), "\n")
			if sent == "" || len(sent) > MaxToolOutputBytes || strings.Contains(sent, "TEST_ONLY_FIXED_VALUE") {
				t.Fatalf("unsafe compress tool output: field=%s oversized=%v bytes=%d", field, oversized, len(sent))
			}
			if oversized && !strings.Contains(sent, "…") {
				t.Fatal("oversized compress output omitted without a marker")
			}
		}
	}
}
