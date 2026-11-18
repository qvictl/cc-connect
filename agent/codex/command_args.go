package codex

import (
	"fmt"
	"strings"
)

// codexSubcommandArgs keeps config overrides in a single parser scope. Codex
// 0.155.1 drops parent overrides when the subcommand also supplies overrides.
// Keep configured overrides first so explicit session settings still win, and
// leave profiles and other command arguments in their original parent scope.
// Only recognized config option forms are moved; values are never interpreted.
func codexSubcommandArgs(extra, command []string) ([]string, error) {
	parent := make([]string, 0, len(extra))
	config := make([]string, 0, len(extra))
	for i := 0; i < len(extra); i++ {
		arg := extra[i]
		switch {
		case arg == "--":
			parent = append(parent, extra[i:]...)
			i = len(extra)
		case arg == "-c" || arg == "--config":
			if i+1 >= len(extra) || !validCodexConfigOverride(extra[i+1]) {
				return nil, fmt.Errorf("codex: %s requires a key=value override", arg)
			}
			config = append(config, arg, extra[i+1])
			i++
		case strings.HasPrefix(arg, "--config=") || strings.HasPrefix(arg, "-c="):
			if !validCodexConfigOverride(strings.SplitN(arg, "=", 2)[1]) {
				return nil, fmt.Errorf("codex: config option requires a key=value override")
			}
			config = append(config, arg)
		case (arg == "-p" || arg == "--profile") && i+1 < len(extra):
			parent = append(parent, arg, extra[i+1])
			i++
		default:
			parent = append(parent, arg)
		}
	}
	if len(command) == 0 {
		return append(parent, config...), nil
	}
	scope := 1
	if len(command) > 1 && command[0] == "exec" && command[1] == "resume" {
		scope = 2
	}
	args := append(parent, command[:scope]...)
	args = append(args, config...)
	return append(args, command[scope:]...), nil
}

func validCodexConfigOverride(value string) bool {
	key, _, ok := strings.Cut(value, "=")
	return ok && strings.TrimSpace(key) != "" && !strings.HasPrefix(key, "-")
}
