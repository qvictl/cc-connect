package codex

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestAppServerSession_ResumeFailureIncludesOperationContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	stdin := &lockedWriteCloser{}
	s := &appServerSession{
		ctx:     ctx,
		stdin:   stdin,
		pending: make(map[int64]chan rpcResponseEnvelope),
	}
	done := make(chan error, 1)
	go func() {
		done <- s.ensureThread("thread-existing")
	}()

	var request struct {
		ID     int64  `json:"id"`
		Method string `json:"method"`
	}
	if err := json.Unmarshal([]byte(waitForWrittenJSONLine(t, stdin)), &request); err != nil {
		t.Fatalf("decode request: %v", err)
	}
	if request.Method != "thread/resume" {
		t.Fatalf("method = %q, want thread/resume", request.Method)
	}

	// A version-specific parameter rejection must remain identifiable in the
	// engine's existing fallback log without adding a duplicate adapter log.
	const cause = "unrecognized field excludeTurns"
	s.handleResponse(rpcResponseEnvelope{
		ID:    request.ID,
		Error: &rpcError{Code: -32602, Message: cause},
	})

	select {
	case err := <-done:
		const want = "codex app-server thread/resume: " + cause
		if err == nil || err.Error() != want {
			t.Fatalf("ensureThread() error = %v, want %q", err, want)
		}
		if unwrapped := errors.Unwrap(err); unwrapped == nil || unwrapped.Error() != cause {
			t.Fatalf("underlying error = %v, want %q", unwrapped, cause)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for failed thread resume")
	}
	if got := s.CurrentSessionID(); got != "" {
		t.Fatalf("failed resume set thread ID = %q", got)
	}
	if got := strings.Count(stdin.String(), "\n"); got != 1 {
		t.Fatalf("sent %d requests, want only the failed resume", got)
	}
}
