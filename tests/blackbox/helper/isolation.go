package helper

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Environment variables cannot opt in to live provider traffic. The caller
// must explicitly supply this test-binary flag for an authorized live run.
var allowLiveProviders = flag.Bool("blackbox-live", false, "allow explicitly authorized live-provider blackbox tests")

func needsLiveProvider(o Options) bool {
	return !o.FakeModel && !o.NoCredentials
}

// binaryEnvironment never copies the parent environment. Only executable
// lookup and Windows OS roots are inherited; credential, proxy, loader and
// agent-config overrides are absent. This is hygiene, not an OS sandbox.
func binaryEnvironment(home string, getenv func(string) string) []string {
	env := []string{
		"HOME=" + home,
		"USERPROFILE=" + home,
		"HOMEDRIVE=" + filepath.VolumeName(home),
		"HOMEPATH=" + strings.TrimPrefix(home, filepath.VolumeName(home)),
		"XDG_CONFIG_HOME=" + filepath.Join(home, ".config"),
		"XDG_CACHE_HOME=" + filepath.Join(home, ".cache"),
		"XDG_DATA_HOME=" + filepath.Join(home, ".local", "share"),
		"XDG_STATE_HOME=" + filepath.Join(home, ".local", "state"),
		"APPDATA=" + filepath.Join(home, ".config"),
		"LOCALAPPDATA=" + filepath.Join(home, ".cache"),
		"CODEX_HOME=" + filepath.Join(home, ".codex"),
		"CLAUDE_CONFIG_DIR=" + filepath.Join(home, ".claude"),
		"TMPDIR=" + filepath.Join(home, "tmp"),
		"TMP=" + filepath.Join(home, "tmp"),
		"TEMP=" + filepath.Join(home, "tmp"),
		"NO_PROXY=127.0.0.1,localhost,::1",
		"no_proxy=127.0.0.1,localhost,::1",
	}
	for _, name := range []string{"PATH", "SystemRoot", "WINDIR"} {
		if value := getenv(name); value != "" {
			env = append(env, name+"="+value)
		}
	}
	return env
}

func prepareBinaryHome(home string) error {
	for _, dir := range []string{"", ".config", ".cache", ".local/share", ".local/state", ".codex", ".claude", "tmp"} {
		if err := os.MkdirAll(filepath.Join(home, dir), 0o700); err != nil {
			return fmt.Errorf("blackbox: private directory: %w", err)
		}
	}
	return nil
}
