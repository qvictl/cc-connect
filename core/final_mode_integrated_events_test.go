package core

import (
	"reflect"
	"testing"
)

func TestFinalModeSuppressesIntegratedProgressEvents(t *testing.T) {
	p := &cardRecorderPlatform{stubPlatformEngine: stubPlatformEngine{n: "test"}}
	e := newFinalModeEngine(t, p)
	enabled := true
	e.SetDisplayConfig(DisplayCfg{Mode: "final", CommentaryMessages: &enabled})
	session := runFinalModeTurn(t, e, p, "test:final-integrated",
		Event{Type: EventCommentary, Content: "parent progress"},
		Event{Type: EventSubagent, Subagent: &SubagentEvent{ID: "child", Phase: SubagentStarted, Title: "Child", Prompt: "work"}},
		Event{Type: EventSubagent, Subagent: &SubagentEvent{ID: "child", Phase: SubagentStep, Step: ProgressCardEntry{Kind: ProgressEntryInfo, Text: "child progress"}}},
		Event{Type: EventSubagent, Subagent: &SubagentEvent{ID: "child", Phase: SubagentFinished, Status: "completed"}},
		Event{Type: EventGoal, Goal: &Goal{Status: "active", Objective: "Keep working"}},
		Event{Type: EventResult, Done: true, Content: "Final answer"},
	)
	if got := p.getSent(); !reflect.DeepEqual(got, []string{"Final answer"}) {
		t.Fatalf("final mode messages = %v", got)
	}
	if cards := p.cards(t); len(cards) != 0 {
		t.Fatalf("final mode created subagent cards: %+v", cards)
	}
	if !session.GetResumeGoal() {
		t.Fatal("suppressing goal notice lost resume state")
	}
}

func TestFinalModeBackgroundHelpersPreserveGoalStateWithoutOutput(t *testing.T) {
	p := &cardRecorderPlatform{stubPlatformEngine: stubPlatformEngine{n: "test"}}
	e := newFinalModeEngine(t, p)
	session := e.sessions.GetOrCreateActive("test:final-background-integrated")
	state := &interactiveState{platform: p, replyCtx: "ctx"}
	e.relayCommentary(p, "ctx", "background progress", "")
	e.handleSubagentEvent(state, Event{Type: EventSubagent, Subagent: &SubagentEvent{ID: "child", Phase: SubagentStarted}})
	if state.subagents != nil {
		t.Fatal("final mode allocated a subagent card worker")
	}
	e.relayGoalEvent(state, session, e.sessions, Event{Type: EventGoal, Goal: &Goal{Status: "active"}})
	if !session.GetResumeGoal() || state.goalSessionStore != e.sessions {
		t.Fatal("goal persistence bookkeeping skipped")
	}
	e.relayGoalEvent(state, session, e.sessions, Event{Type: EventGoal})
	if session.GetResumeGoal() {
		t.Fatal("cleared goal still resumes")
	}
	if got := p.getSent(); len(got) != 0 {
		t.Fatalf("background helpers leaked progress: %v", got)
	}
}
