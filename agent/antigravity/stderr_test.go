package antigravity

import (
	"strings"
	"testing"
)

// These tests use only locally constructed text and never launch a process,
// read the host environment, send requests, or print unredacted test input.
func TestRedactAgyStderr_UsesProcessEnvironmentValues(t *testing.T) {
	value := strings.Repeat("x", 12)
	env := []string{agyTestEnvEntry("provider_token", value), agyTestEnvEntry("MODEL", "ordinary"), agyTestEnvEntry("EMPTY_SECRET", ""), "MALFORMED"}
	got := redactAgyStderr("failure: "+value+"; ordinary", env)
	if got != "failure: [redacted]; ordinary" {
		t.Fatal("process environment value was not redacted or ordinary context changed")
	}
	if env[0] != agyTestEnvEntry("provider_token", value) {
		t.Fatal("redaction mutated the process environment")
	}
}

func TestRedactAgyStderr_OverlappingValuesAndEmbeddedEquals(t *testing.T) {
	short := strings.Repeat("x", 4)
	long := short + "=" + strings.Repeat("y", 4)
	got := redactAgyStderr(long+" "+short, []string{agyTestEnvEntry("API_KEY", short), agyTestEnvEntry("PASSWORD", long)})
	if got != "[redacted] [redacted]" {
		t.Fatal("overlapping values or embedded equals were not fully redacted")
	}
}

func TestRedactAgyStderr_AllSensitiveEnvironmentNames(t *testing.T) {
	for _, key := range []string{"API_KEY", "TOKEN", "CLIENT_SECRET", "PASSWORD", "PASSWD", "CREDENTIAL", "AUTH", "COOKIE"} {
		value := strings.Repeat("z", 8)
		if redactAgyStderr(value, []string{agyTestEnvEntry(key, value)}) != "[redacted]" {
			t.Fatal("sensitive environment name was not recognized")
		}
	}
}

func TestRedactAgyStderr_PreservesOrdinaryDiagnostics(t *testing.T) {
	const message = "process failed: invalid model selection"
	if redactAgyStderr(message, nil) != message {
		t.Fatal("ordinary diagnostic changed")
	}
}

func agyTestEnvEntry(key, value string) string {
	return key + "=" + value
}
