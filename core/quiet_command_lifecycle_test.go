package core

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func quietShellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

// The child holds inherited output pipes without writing to them. The test
// releases its own fixture and waits for its completion signal; a bounded loop
// limits its lifetime if the test process is interrupted. No process-group kill is
// used: successful commands may intentionally start background work.
func quietInheritedPipeCommand(t *testing.T, prefix string, exitCode int) string {
	t.Helper()
	dir := t.TempDir()
	started := filepath.Join(dir, "started")
	release := filepath.Join(dir, "release")
	finished := filepath.Join(dir, "finished")
	t.Cleanup(func() {
		if err := os.WriteFile(release, nil, 0o600); err != nil {
			t.Error(err)
			return
		}
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			if _, err := os.Stat(finished); err == nil {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Error("quiet pipe-holder fixture did not finish after release")
	})
	// Complete a child-start handshake before the parent exits. Cleanup must
	// not mistake a not-yet-scheduled child for one that was never created.
	return fmt.Sprintf("(printf started > %s; i=0; while [ ! -e %s ] && [ \"$i\" -lt 200 ]; do i=$((i+1)); sleep 0.05; done; printf finished > %s) & while [ ! -e %s ]; do sleep 0.01; done; %s; exit %d",
		quietShellQuote(started), quietShellQuote(release), quietShellQuote(finished), quietShellQuote(started), prefix, exitCode)
}

func TestEngine_QuietExecCommandPreservesExitAndBothStreams(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires a POSIX shell")
	}
	for _, tc := range []struct {
		name    string
		command func(*testing.T) string
		want    []string
		fail    bool
	}{
		{"successful_shell_with_inherited_child_pipe", func(t *testing.T) string { return quietInheritedPipeCommand(t, "echo done", 0) }, []string{"done"}, false},
		{"failure_preserves_stdout_and_stderr", func(*testing.T) string { return "echo OUT; echo ERR >&2; exit 2" }, []string{"exit code 2", "OUT", "ERR"}, true},
		{"failed_shell_with_inherited_child_pipe", func(t *testing.T) string { return quietInheritedPipeCommand(t, "echo OUT; echo ERR >&2", 2) }, []string{"exit code 2", "OUT", "ERR"}, true},
		{"failure_stdout_only", func(*testing.T) string { return "echo OUT; exit 2" }, []string{"exit code 2", "OUT"}, true},
		{"failure_stderr_only", func(*testing.T) string { return "echo ERR >&2; exit 2" }, []string{"exit code 2", "ERR"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newTestEngine()
			t.Cleanup(e.cancel)
			p := &stubPlatformEngine{n: "test"}
			start := time.Now()
			err := e.runShellQuiet(p, "ctx", tc.command(t), t.TempDir(), 5*time.Second, 4000)
			if time.Since(start) >= 5*time.Second {
				t.Fatal("quiet command waited for its background child's lifetime")
			}
			if (err != nil) != tc.fail {
				t.Fatalf("error = %v, want failure %v", err, tc.fail)
			}
			sent := p.getSent()
			if len(sent) != 1 {
				t.Fatalf("expected one reply, got %q", sent)
			}
			for _, want := range tc.want {
				if !strings.Contains(sent[0], want) {
					t.Errorf("reply %q missing %q", sent[0], want)
				}
			}
			if !tc.fail && sent[0] != "done" {
				t.Errorf("successful shell reported as failure or framed: %q", sent[0])
			}
		})
	}
}

func TestEngine_QuietExecTimeoutAndCancellationRemainFailures(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires a POSIX shell")
	}
	t.Run("deadline", func(t *testing.T) {
		e := newTestEngine()
		defer e.cancel()
		p := &stubPlatformEngine{n: "test"}
		dir := t.TempDir()
		started := filepath.Join(dir, "started")
		// exec replaces the shell, so cancellation reaps the actual sleeper.
		err := e.runShellQuiet(p, "ctx", "printf BEFORE; printf started > "+quietShellQuote(started)+"; exec sleep 30", dir, 500*time.Millisecond, 4000)
		if err == nil || !strings.Contains(err.Error(), "timed out") {
			t.Fatalf("timeout error = %v", err)
		}
		got := p.getSent()
		if len(got) != 1 {
			t.Fatalf("timeout replies = %q", got)
		}
		if _, err := os.Stat(started); err == nil && !strings.Contains(got[0], "BEFORE") {
			t.Fatalf("timeout lost output written before the startup signal: %q", got)
		}
	})
	t.Run("cancel_running_parent", func(t *testing.T) {
		e := newTestEngine()
		defer e.cancel()
		p := &stubPlatformEngine{n: "test"}
		dir := t.TempDir()
		started := filepath.Join(dir, "started")
		done := make(chan error, 1)
		go func() {
			done <- e.runShellQuiet(p, "ctx", "printf started > "+quietShellQuote(started)+"; exec sleep 30", dir, 10*time.Second, 4000)
		}()
		deadline := time.Now().Add(5 * time.Second)
		for {
			if _, err := os.Stat(started); err == nil {
				break
			}
			if time.Now().After(deadline) {
				e.cancel()
				select {
				case <-done:
				case <-time.After(5 * time.Second):
				}
				t.Fatal("quiet fixture did not start")
			}
			time.Sleep(10 * time.Millisecond)
		}
		e.cancel()
		select {
		case err := <-done:
			if err == nil {
				t.Fatal("cancelled command reported success")
			}
		case <-time.After(5 * time.Second):
			t.Fatal("cancelled direct child was not reaped promptly")
		}
		// A cancelled platform context may suppress delivery; do not demand a
		// chat message after shutdown. The process result must still fail.
	})
}

type quietPreviewPlatform struct {
	stubPlatformEngine
	previews atomic.Int32
	updates  atomic.Int32
}

func (p *quietPreviewPlatform) SendPreviewStart(context.Context, any, string) (any, error) {
	p.previews.Add(1)
	return "preview", nil
}
func (p *quietPreviewPlatform) UpdateMessage(context.Context, any, string) error {
	p.updates.Add(1)
	return nil
}

func TestEngine_QuietExecNeverStartsCardProgress(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires a POSIX shell")
	}
	e := newTestEngine()
	defer e.cancel()
	p := &quietPreviewPlatform{stubPlatformEngine: stubPlatformEngine{n: "test"}}
	e.executeShellCommand(p, &Message{ReplyCtx: "ctx"}, &CustomCommand{Exec: "sleep 0.6; printf done", WorkDir: t.TempDir(), Quiet: true}, nil)
	if got := p.getSent(); len(got) != 1 || got[0] != "done" {
		t.Fatalf("quiet card platform replies = %q", got)
	}
	if p.previews.Load() != 0 || p.updates.Load() != 0 {
		t.Fatal("quiet command started or updated progress")
	}
}
