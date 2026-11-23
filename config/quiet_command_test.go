package config

import (
	"testing"

	"github.com/BurntSushi/toml"
)

// A [[commands]] entry may set quiet; it defaults to false.
func TestCommandConfig_Quiet(t *testing.T) {
	raw := `
[[commands]]
name = "deploy"
exec = "./deploy.sh {{args}}"
quiet = true

[[commands]]
name = "status"
exec = "git status"
`
	var cfg Config
	if _, err := toml.Decode(raw, &cfg); err != nil {
		t.Fatalf("decode config: %v", err)
	}
	if len(cfg.Commands) != 2 || !cfg.Commands[0].Quiet || cfg.Commands[1].Quiet {
		t.Fatalf("commands = %+v", cfg.Commands)
	}
}
