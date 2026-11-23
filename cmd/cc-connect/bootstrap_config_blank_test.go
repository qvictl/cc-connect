package main

import (
	"path/filepath"
	"testing"

	"github.com/BurntSushi/toml"
	"github.com/chenhg5/cc-connect/config"
)

func TestBootstrapConfigLeavesFeishuSecretUnfilled(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := bootstrapConfig(path); err != nil {
		t.Fatal(err)
	}
	var cfg config.Config
	if _, err := toml.DecodeFile(path, &cfg); err != nil {
		t.Fatalf("decode starter config: %v", err)
	}
	if len(cfg.Projects) != 1 || len(cfg.Projects[0].Platforms) != 1 {
		t.Fatal("starter config must still contain a Feishu platform")
	}
	p := cfg.Projects[0].Platforms[0]
	if p.Type != "feishu" || p.Options["app_secret"] != "" {
		t.Fatal("starter Feishu config must leave the secret unfilled")
	}
}
