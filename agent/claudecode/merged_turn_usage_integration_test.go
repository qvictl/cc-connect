package claudecode

import (
	"reflect"
	"testing"
	"time"

	"github.com/chenhg5/cc-connect/core"
)

func TestMergedBusyInputPreservesSidechainUsage(t *testing.T) {
	cs := newMidTurnTestSession(t, time.Hour)
	first := core.Event{Type: core.EventResult, Done: true, Content: "first", InputTokens: 2,
		ReasoningOutputTokens: 3, UsageIncomplete: true,
		Sidechain: &core.SidechainUsage{Requests: 1, InputTokens: 10, OutputTokens: 20,
			CacheCreationInputTokens: 30, CacheReadInputTokens: 40, ReasoningOutputTokens: 5,
			Models: []string{"b", "a"}, ByModel: map[string]core.TokenUsage{"a": {InputTokens: 10}}}}
	second := core.Event{Type: core.EventResult, Done: true, Content: "second", InputTokens: 7,
		ReasoningOutputTokens: 4,
		Sidechain: &core.SidechainUsage{Requests: 2, InputTokens: 11, OutputTokens: 21,
			CacheCreationInputTokens: 31, CacheReadInputTokens: 41, ReasoningOutputTokens: 6,
			Models: []string{"b", "c"}, ByModel: map[string]core.TokenUsage{"a": {InputTokens: 11}, "c": {OutputTokens: 21}}}}
	cs.midTurnPending = []uint64{1}
	cs.deliverResult(first)
	cs.followUpTurnStarted()
	cs.deliverResult(second)
	got := <-cs.events
	wantChild := &core.SidechainUsage{Requests: 3, InputTokens: 21, OutputTokens: 41,
		CacheCreationInputTokens: 61, CacheReadInputTokens: 81, ReasoningOutputTokens: 11,
		Models: []string{"a", "b", "c"}, ByModel: map[string]core.TokenUsage{"a": {InputTokens: 21}, "c": {OutputTokens: 21}}}
	if !reflect.DeepEqual(got.Sidechain, wantChild) {
		t.Fatalf("sidechain = %+v, want %+v", got.Sidechain, wantChild)
	}
	if got.Content != "first\n\nsecond" || got.InputTokens != 9 || got.ReasoningOutputTokens != 7 || !got.UsageIncomplete {
		t.Fatalf("merged result = %+v", got)
	}
	if first.Sidechain.ByModel["a"].InputTokens != 10 || second.Sidechain.ByModel["a"].InputTokens != 11 {
		t.Fatal("merge mutated source usage")
	}
	select {
	case extra := <-cs.events:
		t.Fatalf("unexpected duplicate result: %+v", extra)
	default:
	}
}

func TestMergeTurnResultsPreservesOneSidedUsage(t *testing.T) {
	for _, firstHasUsage := range []bool{false, true} {
		first, second := core.Event{}, core.Event{}
		usage := &core.SidechainUsage{InputTokens: 13, Models: []string{"a"}, ByModel: map[string]core.TokenUsage{"a": {InputTokens: 13}}}
		if firstHasUsage {
			first.Sidechain = usage
		} else {
			second.Sidechain = usage
		}
		got := mergeTurnResults(first, second)
		if !reflect.DeepEqual(got.Sidechain, usage) || got.Sidechain == usage {
			t.Fatalf("one-sided usage not independently preserved: %+v", got.Sidechain)
		}
	}
	if got := mergeTurnResults(core.Event{}, core.Event{}); got.Sidechain != nil {
		t.Fatal("empty results gained sidechain usage")
	}
}

func TestHeldResultIgnoresBackgroundTaskLifecycle(t *testing.T) {
	cs := newMidTurnTestSession(t, time.Hour)
	cs.midTurnPending = []uint64{1}
	cs.deliverResult(core.Event{Type: core.EventResult, Done: true, Content: "first"})
	for _, line := range []string{
		`{"type":"system","subtype":"task_started","task_type":"local_agent","tool_use_id":"child","is_backgrounded":true}`,
		`{"type":"system","subtype":"task_notification","tool_use_id":"child","status":"completed"}`,
		`{"type":"result","subtype":"success","result":"","num_turns":0,"origin":{"kind":"task-notification"}}`,
	} {
		cs.handleReadLoopLine(line)
	}
	for len(cs.events) > 0 {
		if event := <-cs.events; event.Type == core.EventResult {
			t.Fatalf("sideband task released foreground result: %+v", event)
		}
	}
	cs.handleReadLoopLine(initLine)
	cs.deliverResult(core.Event{Type: core.EventResult, Done: true, Content: "second"})
	var results []core.Event
	for len(cs.events) > 0 {
		if event := <-cs.events; event.Type == core.EventResult {
			results = append(results, event)
		}
	}
	if len(results) != 1 || results[0].Content != "first\n\nsecond" {
		t.Fatalf("expected one merged foreground result, got %+v", results)
	}
}
