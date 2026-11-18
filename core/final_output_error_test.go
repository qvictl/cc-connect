package core

import (
	"errors"
	"fmt"
	"slices"
	"testing"
)

func TestUnsolicitedReader_ErrorHonorsDisplayAndResetsSegments(t *testing.T) {
	const narration = "Let me check."
	const answer = "The answer is 42."
	for _, tc := range []struct {
		name    string
		display DisplayCfg
		partial string
	}{
		{"final", DisplayCfg{Mode: "final"}, answer},
		{"quiet hides footer", DisplayCfg{Mode: "quiet", HideAgentFooter: true}, narration + answer},
		{"quiet keeps footer", DisplayCfg{Mode: "quiet"}, narration + answer + comboFooterHead + comboFooterTail},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sent := runBackgroundTurn(t, tc.display, true,
				Event{Type: EventText, Content: narration},
				Event{Type: EventToolUse, ToolName: "Bash"},
				Event{Type: EventText, Content: answer},
				Event{Type: EventText, Content: comboFooterHead},
				Event{Type: EventText, Content: comboFooterTail},
				Event{Type: EventError, Error: errors.New("background failed")},
				Event{Type: EventText, Content: "Hello "},
				Event{Type: EventText, Content: "world."},
			)
			want := []string{tc.partial, fmt.Sprintf(NewI18n(LangEnglish).T(MsgError), "background failed"), "Hello world."}
			if !slices.Equal(sent, want) {
				t.Fatalf("sent = %q, want %q", sent, want)
			}
		})
	}
}

// A chunk can already match the footer regexp, leaving the other chunk
// (for example a lone closing star) to replace the real answer.
func TestFinalMode_FooterSplitAtEveryBoundary(t *testing.T) {
	footer := "\n" + agentStatusFooter
	for cut := range footer {
		if cut == 0 {
			continue
		}
		t.Run(fmt.Sprintf("split-%d", cut), func(t *testing.T) {
			tc := comboCase{
				name:   fmt.Sprintf("split-%d", cut),
				answer: comboAnswer,
				events: []Event{
					{Type: EventText, Content: comboAnswer},
					{Type: EventThinking, Content: "verified"},
					{Type: EventText, Content: footer[:cut]},
					{Type: EventText, Content: footer[cut:]},
				},
			}
			checkCombo(t, "foreground result", tc, runForegroundCombo(t, tc, false), nil)
			checkCombo(t, "foreground channel closed", tc, runForegroundCombo(t, tc, true), nil)
			checkCombo(t, "background result", tc, runBackgroundCombo(t, tc, "result"), nil)
			checkCombo(t, "background partial flush", tc, runBackgroundCombo(t, tc, "partial"), nil)
		})
	}
}

// Some platforms use this callback to remove their busy indicator.
func TestFinalMode_KeepsDoneReactionCleanup(t *testing.T) {
	p := &stubDoneReactionPlatform{stubPlatformEngine: stubPlatformEngine{n: "test"}}
	e := newFinalModeEngine(t, p)
	runFinalModeTurn(t, e, p, "test:final-cleanup", Event{Type: EventResult, Content: "done", Done: true})
	if count, _ := p.doneSnapshot(); count != 1 {
		t.Fatalf("done cleanup calls = %d, want 1", count)
	}
}
