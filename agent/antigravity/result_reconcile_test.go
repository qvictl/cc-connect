package antigravity

import (
	"strconv"
	"strings"
	"testing"

	"github.com/chenhg5/cc-connect/core"
)

func TestAgyOutputParser_HistoricalErrorRequiresTerminalResponse(t *testing.T) {
	for _, tc := range []struct {
		name      string
		delta     string
		response  string
		toolAfter bool
		wantText  string
		wantError bool
	}{
		{"terminal confirms final delta", "done", "done", false, "done", false},
		{"delta alone does not prove success", "done", "", false, "done", true},
		{"blank delta", " \t", "", false, " \t", true},
		{"commentary before tool", "Working", "", true, "", true},
		{"no answer", "", "", false, "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := newAgyOutputParser()
			delta := `{"event":"step_update","step_update":{"step_index":0,"step_type":"agent_response","text_delta":` +
				strconv.Quote(tc.delta) + `}}`
			_, events := p.parseLine([]byte(delta))
			var all []core.Event
			all = append(all, events...)
			if tc.toolAfter {
				_, events = p.parseLine([]byte(`{"event":"step_update","step_update":{"step_index":1,"step_type":"tool","tool_name":"read_file"}}`))
				all = append(all, events...)
			}
			terminal := `{"event":"result","result":{"status":"ERROR","response":` +
				strconv.Quote(tc.response) + `,"error":"prior or current error"}}`
			_, events = p.parseLine([]byte(terminal))
			all = append(all, events...)
			var text strings.Builder
			errors := 0
			for _, event := range all {
				switch event.Type {
				case core.EventText:
					text.WriteString(event.Content)
				case core.EventError:
					errors++
				}
			}
			if text.String() != tc.wantText {
				t.Fatalf("final text = %q, want %q", text.String(), tc.wantText)
			}
			wantErrors := 0
			if tc.wantError {
				wantErrors = 1
			}
			if errors != wantErrors {
				t.Fatalf("errors = %d, want %d", errors, wantErrors)
			}
			_, events = p.parseLine([]byte(`{"event":"result","result":{"response":"duplicate","error":"late error"}}`))
			if len(events) != 0 {
				t.Fatal("duplicate terminal event was not ignored")
			}
		})
	}
}
