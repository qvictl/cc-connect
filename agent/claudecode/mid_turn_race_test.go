package claudecode

import (
	"io"
	"testing"
	"time"

	"github.com/chenhg5/cc-connect/core"
)

// resultOnWrite emits the current turn's result synchronously while
// SendMidTurn is still inside Send, reproducing the read-loop race where the
// result arrives before the mid-turn counter used to be incremented.
type resultOnWrite struct {
	cs      *claudeSession
	emitted bool
}

func (w *resultOnWrite) Write(p []byte) (int, error) {
	if !w.emitted {
		w.emitted = true
		w.cs.handleReadLoopLine(resultA)
	}
	return len(p), nil
}

func (w *resultOnWrite) Close() error { return nil }

func doneResults(events []core.Event) []core.Event {
	var out []core.Event
	for _, evt := range events {
		if evt.Type == core.EventResult && evt.Done {
			out = append(out, evt)
		}
	}
	return out
}

// A result that arrives while Send is writing must still be held and merged
// with the follow-up turn, not emitted before the counter is bumped.
func TestClaudeSession_ResultDuringSendIsHeld(t *testing.T) {
	cs := newMidTurnTestSession(t, time.Hour)
	cs.stdin = &resultOnWrite{cs: cs}

	if err := cs.SendMidTurn("add BANANA", "", nil, nil); err != nil {
		t.Fatal(err)
	}
	if early := doneResults(drain(cs, 20*time.Millisecond)); len(early) != 0 {
		t.Fatalf("result emitted before the follow-up: %#v", early)
	}

	cs.handleReadLoopLine(initLine)
	cs.handleReadLoopLine(textB)
	cs.handleReadLoopLine(resultB)

	got := doneResults(drain(cs, 50*time.Millisecond))
	if len(got) != 1 || got[0].Content != "STORY\n\nBANANA." {
		t.Fatalf("results = %#v, want one merged STORY + BANANA result", got)
	}
}

// A mid-turn compaction result is not a turn terminator: it must not release
// the held result before the follow-up turn starts.
func TestClaudeSession_CompactionDoesNotReleaseHeldResult(t *testing.T) {
	cs := newMidTurnTestSession(t, time.Hour)
	if err := cs.SendMidTurn("add BANANA", "", nil, nil); err != nil {
		t.Fatal(err)
	}
	cs.handleReadLoopLine(resultA)
	cs.handleReadLoopLine(`{"type":"result","subtype":"compact","is_error":false,"result":"compacted","session_id":"test-session","usage":{"input_tokens":1,"output_tokens":1}}`)

	// The compaction event itself may be emitted (Done=false); the held turn
	// result must not be.
	if early := doneResults(drain(cs, 20*time.Millisecond)); len(early) != 0 {
		t.Fatalf("held turn result released by compaction: %#v", early)
	}

	cs.handleReadLoopLine(initLine)
	cs.handleReadLoopLine(textB)
	cs.handleReadLoopLine(resultB)

	got := doneResults(drain(cs, 50*time.Millisecond))
	if len(got) != 1 || got[0].Content != "STORY\n\nBANANA." {
		t.Fatalf("results = %#v, want one merged STORY + BANANA result", got)
	}
}

// A second result arriving before the held one is merged must not overwrite
// it, and the first result's timer must not release the newer held result.
func TestClaudeSession_SecondHeldResultMerges(t *testing.T) {
	cs := newMidTurnTestSession(t, 100*time.Millisecond)
	if err := cs.SendMidTurn("first", "", nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := cs.SendMidTurn("second", "", nil, nil); err != nil {
		t.Fatal(err)
	}

	cs.handleReadLoopLine(resultA)
	time.Sleep(30 * time.Millisecond)
	cs.handleReadLoopLine(resultB)

	got := doneResults(drain(cs, 300*time.Millisecond))
	if len(got) != 1 || got[0].Content != "STORY\n\nBANANA." {
		t.Fatalf("results = %#v, want one merged STORY + BANANA result", got)
	}
}

var _ io.WriteCloser = (*resultOnWrite)(nil)
