package core

import (
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

// waitSent waits up to d for p to have sent at least n messages.
func waitSent(p *stubPlatformEngine, n int, d time.Duration) []string {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if sent := p.getSent(); len(sent) >= n {
			return sent
		}
		time.Sleep(20 * time.Millisecond)
	}
	return p.getSent()
}

// A regular exec command that prints nothing still answers "(no output)".
func TestEngine_ExecCommand_EmptyOutputStillAnswers(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires a POSIX shell")
	}
	e := newTestEngine()
	e.SetAdminFrom("owner")
	e.commands.Add("loud", "", "", "true", t.TempDir(), "config")
	p := &stubPlatformEngine{n: "test"}
	e.handleCommand(p, &Message{SessionKey: "test:owner", UserID: "owner", ReplyCtx: "ctx"}, "/loud")
	sent := waitSent(p, 1, 5*time.Second)
	if len(sent) != 1 || !strings.Contains(sent[0], "(no output)") {
		t.Fatalf("expected the (no output) reply, got %q", sent)
	}
}

// A quiet command sends nothing for an empty stdout, no progress message
// while it runs, its stdout as plain text, and a failure as an error.
func TestEngine_QuietExecCommand(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires a POSIX shell")
	}
	e := newTestEngine()
	e.SetAdminFrom("owner")
	dir := t.TempDir()
	// The sleeps outlast quickFinishTimeout, past which a regular exec
	// command would post a progress message.
	e.commands.Add("silent", "", "", "sleep 0.8", dir, "config", true)
	e.commands.Add("answer", "", "", "sleep 0.6; echo opened {{args}}", dir, "config", true)
	e.commands.Add("broken", "", "", "echo nope >&2; exit 3", dir, "config", true)
	msg := func() *Message {
		return &Message{SessionKey: "test:owner", UserID: "owner", ReplyCtx: "ctx"}
	}

	p := &stubPlatformEngine{n: "test"}
	e.handleCommand(p, msg(), "/silent")
	time.Sleep(1500 * time.Millisecond)
	if sent := p.getSent(); len(sent) != 0 {
		t.Fatalf("a quiet command with no output sent %q", sent)
	}

	e.handleCommand(p, msg(), "/answer debug")
	sent := waitSent(p, 1, 5*time.Second)
	if len(sent) != 1 || sent[0] != "opened debug" {
		t.Fatalf("expected only the plain answer, got %q", sent)
	}

	p.clearSent()
	e.handleCommand(p, msg(), "/broken")
	sent = waitSent(p, 1, 5*time.Second)
	if len(sent) != 1 || !strings.Contains(sent[0], "exit code 3") || !strings.Contains(sent[0], "nope") {
		t.Fatalf("expected the failure reported, got %q", sent)
	}
}

// Re-registration must publish Quiet with the command, not mutate a pointer
// already being consumed by executeShellCommand. Run this regression with -race.
func TestEngine_QuietCommandConcurrentRegistrationAndExecution(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires a POSIX shell")
	}
	e := newTestEngine()
	dir := t.TempDir()
	e.AddCommand("quiet", "", "", "true", dir, "config", true)
	original, ok := e.commands.Resolve("quiet")
	if !ok || !original.Quiet {
		t.Fatal("quiet flag was not initialized at registration")
	}
	p := &stubPlatformEngine{n: "test"}
	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		for i := 0; i < 500; i++ {
			e.AddCommand("quiet", "", "", "true", dir, "config", true)
		}
	}()
	go func() {
		defer wg.Done()
		<-start
		for i := 0; i < 30; i++ {
			cmd, ok := e.commands.Resolve("quiet")
			if !ok || !cmd.Quiet {
				t.Error("registration exposed a non-quiet command")
				return
			}
			e.executeShellCommand(p, &Message{ReplyCtx: "ctx"}, cmd, nil)
		}
	}()
	close(start)
	wg.Wait()
	if sent := p.getSent(); len(sent) != 0 {
		t.Fatalf("quiet execution during registration sent %q", sent)
	}
	e.AddCommand("quiet", "", "", "true", dir, "config")
	current, ok := e.commands.Resolve("quiet")
	if !ok || current.Quiet || !original.Quiet {
		t.Fatal("replacement mutated a previously resolved command")
	}
}

func TestEngine_QuietExecCommandPreservesExitAndBothStreams(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires a POSIX shell")
	}
	for _, test := range []struct {
		name    string
		command string
		want    []string
		fail    bool
	}{
		{
			name:    "successful_shell_with_inherited_child_pipe",
			command: "echo done; (sleep 3 &)",
			want:    []string{"done"},
		},
		{
			name:    "failure_preserves_stdout_and_stderr",
			command: "echo OUT; echo ERR >&2; exit 2",
			want:    []string{"exit code 2", "OUT", "ERR"},
			fail:    true,
		},
		{
			name:    "failed_shell_with_inherited_child_pipe",
			command: "echo OUT; echo ERR >&2; (sleep 3 &); exit 2",
			want:    []string{"exit code 2", "OUT", "ERR"},
			fail:    true,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			e := newTestEngine()
			p := &stubPlatformEngine{n: "test"}
			err := e.runShellQuiet(p, "ctx", test.command, t.TempDir(), 5*time.Second, 4000)
			if (err != nil) != test.fail {
				t.Fatalf("error = %v, want failure %v", err, test.fail)
			}
			sent := p.getSent()
			if len(sent) != 1 {
				t.Fatalf("expected one reply, got %q", sent)
			}
			for _, want := range test.want {
				if !strings.Contains(sent[0], want) {
					t.Errorf("reply %q missing %q", sent[0], want)
				}
			}
			if !test.fail && sent[0] != "done" {
				t.Errorf("successful shell reported as failure or framed: %q", sent[0])
			}
		})
	}
}
