package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/chenhg5/cc-connect/config"
	"github.com/chenhg5/cc-connect/core"
)

type quietSettingsPlatform struct{ sent chan string }

func (*quietSettingsPlatform) Name() string                    { return "quiet-settings-fixture" }
func (*quietSettingsPlatform) Start(core.MessageHandler) error { return nil }
func (*quietSettingsPlatform) Stop() error                     { return nil }
func (p *quietSettingsPlatform) Reply(_ context.Context, _ any, content string) error {
	p.sent <- content
	return nil
}
func (p *quietSettingsPlatform) Send(ctx context.Context, replyCtx any, content string) error {
	return p.Reply(ctx, replyCtx, content)
}

// Exercise the actual TOML -> startup registration -> reloadConfig -> engine
// dispatch path. Neither a provider CLI nor a messaging account is started.
func TestConfiguredQuietCommandStartupAndReload(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires a POSIX shell")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	writeConfig := func(quietLine string, include bool) *config.Config {
		t.Helper()
		commands := ""
		if include {
			commands = fmt.Sprintf("[[commands]]\nname = \"quietprobe\"\nexec = \"printf configured-output\"\nwork_dir = %q\n%s\n", dir, quietLine)
		}
		raw := fmt.Sprintf("data_dir = %q\n%s\n[[projects]]\nname = \"quiet-project\"\nadmin_from = \"owner\"\n[projects.agent]\ntype = \"quiet-fixture\"\n[[projects.platforms]]\ntype = \"quiet-fixture\"\n", dir, commands)
		if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
			t.Fatal(err)
		}
		cfg, err := config.Load(path)
		if err != nil {
			t.Fatal(err)
		}
		return cfg
	}
	p := &quietSettingsPlatform{sent: make(chan string, 32)}
	e := core.NewEngine("quiet-project", &stubMainAgent{workDir: dir}, []core.Platform{p}, "", core.LangEnglish)
	t.Cleanup(func() {
		if err := e.Stop(); err != nil {
			t.Error(err)
		}
	})
	e.SetAdminFrom("owner")
	cfg := writeConfig("quiet = true", true)
	wireConfiguredCommands(e, cfg.Commands)
	e.AddCommand("runtimeprobe", "", "", "printf runtime-output", dir, "runtime")

	assertReply := func(quiet bool) {
		t.Helper()
		e.ReceiveMessage(p, &core.Message{SessionKey: "quiet-settings-fixture:chat", UserID: "owner", Content: "/quietprobe", ReplyCtx: "ctx"})
		deadline := time.NewTimer(5 * time.Second)
		defer deadline.Stop()
		for {
			select {
			case got := <-p.sent:
				if quiet {
					if got != "configured-output" {
						t.Fatalf("quiet reply = %q", got)
					}
					return
				}
				// A slow CI worker can cross the loud path's progress window.
				// Consume that receipt and wait for the final reply before reload.
				if strings.HasPrefix(got, "⏳") {
					continue
				}
				if !strings.Contains(got, "configured-output") || !strings.Contains(got, "✅") {
					t.Fatalf("default/loud reply = %q", got)
				}
				return
			case <-deadline.C:
				t.Fatal("configured command did not finish")
			}
		}
	}
	assertReply(true)
	for _, setting := range []struct {
		line  string
		quiet bool
	}{
		{"quiet = false", false},
		{"quiet = true", true},
		{"", false},
	} {
		writeConfig(setting.line, true)
		result, err := reloadConfig(path, "quiet-project", e)
		if err != nil {
			t.Fatal(err)
		}
		if result.CommandsUpdated != 1 {
			t.Fatalf("CommandsUpdated = %d", result.CommandsUpdated)
		}
		assertReply(setting.quiet)
	}
	writeConfig("", false)
	result, err := reloadConfig(path, "quiet-project", e)
	if err != nil {
		t.Fatal(err)
	}
	if result.CommandsUpdated != 0 {
		t.Fatalf("removed command count = %d", result.CommandsUpdated)
	}
	foundRuntime := false
	for _, command := range e.GetAllCommands() {
		if command.Command == "quietprobe" {
			t.Fatal("reload retained a removed config command")
		}
		if command.Command == "runtimeprobe" {
			foundRuntime = true
		}
	}
	if !foundRuntime {
		t.Fatal("config reload removed a non-config command")
	}
}
