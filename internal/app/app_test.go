package app

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nlink-jp/brave-search/internal/config"
)

// isolate keeps every test away from the developer's real config and key.
func isolate(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "xdg"))
	for _, v := range config.EnvVars() {
		t.Setenv(v, "")
	}
	return dir
}

// The scaffold's contract with the Homebrew formula: `brew test` runs
// `--version`, so all three spellings must agree byte for byte.
func TestVersionSpellingsAreIdentical(t *testing.T) {
	outputs := make(map[string]string)
	for _, arg := range []string{"version", "--version", "-v"} {
		var stdout, stderr bytes.Buffer
		if code := run([]string{arg}, "1.2.3", nil, &stdout, &stderr); code != exitOK {
			t.Fatalf("%s: exit code = %d, want %d (stderr: %s)", arg, code, exitOK, stderr.String())
		}
		if stderr.Len() != 0 {
			t.Errorf("%s: wrote to stderr: %s", arg, stderr.String())
		}
		outputs[arg] = stdout.String()
	}
	for _, arg := range []string{"--version", "-v"} {
		if outputs[arg] != outputs["version"] {
			t.Errorf("version and %s differ:\n version: %q\n%s: %q", arg, outputs["version"], arg, outputs[arg])
		}
	}
	if !strings.HasPrefix(outputs["version"], "brave-search 1.2.3\n") {
		t.Errorf("version banner does not lead with the binary name and version: %q", outputs["version"])
	}
}

func TestHelpGoesToStdoutAndSucceeds(t *testing.T) {
	for _, arg := range []string{"help", "-h", "--help"} {
		var stdout, stderr bytes.Buffer
		if code := run([]string{arg}, "dev", nil, &stdout, &stderr); code != exitOK {
			t.Errorf("%s: exit code = %d, want %d", arg, code, exitOK)
		}
		if stderr.Len() != 0 {
			t.Errorf("%s: wrote to stderr: %s", arg, stderr.String())
		}
		if !strings.Contains(stdout.String(), "Usage:") {
			t.Errorf("%s: help text has no Usage section", arg)
		}
	}
}

// No arguments and an unknown command are usage errors, and the usage text
// belongs on stderr so a piped stdout stays clean.
func TestUsageErrorsGoToStderr(t *testing.T) {
	for _, args := range [][]string{nil, {"frobnicate"}} {
		var stdout, stderr bytes.Buffer
		if code := run(args, "dev", nil, &stdout, &stderr); code != exitError {
			t.Errorf("%v: exit code = %d, want %d", args, code, exitError)
		}
		if stdout.Len() != 0 {
			t.Errorf("%v: wrote to stdout: %s", args, stdout.String())
		}
		if !strings.Contains(stderr.String(), "Usage:") {
			t.Errorf("%v: no usage text on stderr", args)
		}
	}
}

// Every command the help advertises must exist in dispatch — either
// implemented, or refusing with exit 2 — so the scaffold cannot advertise a
// command that silently succeeds.
func TestEveryAdvertisedCommandDispatches(t *testing.T) {
	isolate(t)
	for _, cmd := range []string{"web", "context", "answer", "research", "auth"} {
		var stdout, stderr bytes.Buffer
		code := run([]string{cmd}, "dev", nil, &stdout, &stderr)
		if code == exitOK && stdout.Len() == 0 {
			t.Errorf("%s: exit 0 with no output — a silent success", cmd)
		}
		if code == exitError && !strings.Contains(stderr.String(), "brave-search:") {
			t.Errorf("%s: exit 2 without a message in the tool's voice: %q", cmd, stderr.String())
		}
	}
}

// The MCP server must start with no key configured: the handshake and
// get_usage are how a client discovers what the operator has to set.
func TestMCPHandshakeWorksWithoutAKey(t *testing.T) {
	isolate(t)
	in := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}` + "\n" +
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}` + "\n")
	var stdout, stderr bytes.Buffer
	if code := run([]string{"mcp"}, "dev", in, &stdout, &stderr); code != exitOK {
		t.Fatalf("mcp exit code = %d (stderr: %s)", code, stderr.String())
	}
	lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("got %d response lines, want 2: %q", len(lines), stdout.String())
	}
	if !strings.Contains(lines[0], `"name":"brave-search"`) {
		t.Errorf("initialize response = %s", lines[0])
	}
	if !strings.Contains(lines[1], `"get_usage"`) {
		t.Errorf("tools/list response = %s", lines[1])
	}
}

func TestMCPRefusesABrokenConfigAtStartup(t *testing.T) {
	dir := isolate(t)
	path := filepath.Join(dir, "bad.toml")
	if err := writeFile(path, "[api]\nnot_a_key = 1\n"); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := run([]string{"mcp", "--config", path}, "dev", strings.NewReader(""), &stdout, &stderr); code != exitError {
		t.Errorf("exit code = %d, want %d", code, exitError)
	}
	if !strings.Contains(stderr.String(), "not_a_key") {
		t.Errorf("stderr does not name the bad key: %s", stderr.String())
	}
}
