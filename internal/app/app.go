// Package app implements the brave-search command-line interface: subcommand
// dispatch plus the web / context / answer / research / auth / mcp commands.
// Core logic lives in the brave, engine and config packages; this package is
// the thin I/O shell around them.
package app

import (
	"fmt"
	"io"
	"os"
)

// Exit codes. Zero results is a successful answer, so it is distinct from an
// operational failure.
const (
	exitOK       = 0 // the query completed (zero results included)
	exitUpstream = 1 // upstream failure, rate limit or timeout — no answer obtained
	exitError    = 2 // usage / validation / configuration error
)

// Run dispatches a subcommand and returns a process exit code.
func Run(args []string, version string) int {
	return run(args, version, os.Stdin, os.Stdout, os.Stderr)
}

// run is Run with injected streams, so dispatch, the version banner and the
// command output can be tested without touching the process's own stdio.
func run(args []string, version string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		usage(stderr)
		return exitError
	}
	cmd, rest := args[0], args[1:]
	switch cmd {
	case "web":
		return runWeb(rest, version, stdout, stderr)
	case "context":
		return runContext(rest, version, stdout, stderr)
	case "answer":
		return runAnswer(rest, version, stdout, stderr)
	case "research":
		return runResearch(rest, version, stdout, stderr)
	case "auth":
		return runAuth(rest, version, stdout, stderr)
	case "mcp":
		return runMCP(rest, version, stdin, stdout, stderr)
	case "version", "--version", "-v":
		printVersion(stdout, version)
		return exitOK
	case "help", "-h", "--help":
		usage(stdout)
		return exitOK
	default:
		fmt.Fprintf(stderr, "unknown command %q\n\n", cmd)
		usage(stderr)
		return exitError
	}
}

// printVersion is the single source of the version banner. `--version` and the
// `version` subcommand must print byte-identical output: a Homebrew formula's
// `brew test` calls `--version`, while humans type `version`.
func printVersion(w io.Writer, version string) {
	fmt.Fprintln(w, "brave-search "+version)
	fmt.Fprintln(w, "Data source: Brave Search API (api.search.brave.com). API key required.")
}

func usage(w io.Writer) {
	fmt.Fprint(w, `brave-search — the Brave Search API as a CLI and a local MCP server

Usage:
  brave-search <command> [flags] <query>

Commands:
  web <query>              Web Search: ranked results with snippets
  context <query>          LLM Context: pre-extracted page chunks for grounding
  answer <question>        Answers: a grounded answer with citations (one search)
  research <question>      Answers research mode: multi-step, multi-search answer
  auth check               Verify the API key and which plans it unlocks
  mcp                      Run as a local MCP server (stdio)
  version                  Print the version

Shared flags:
  -j, --json               JSON output
  --timeout <dur>          Deadline per exchange (e.g. 10s; default 30s,
                           research: max-seconds + 30s)
  -c, --config <path>      Config file (default: first of
                           $XDG_CONFIG_HOME/brave-search/config.toml,
                           ~/.config/brave-search/config.toml,
                           ~/Library/Application Support/brave-search/config.toml)
  --country <cc>           Two-letter country code or ALL (default US)
  --lang <code>            Search language (web, context) or reply language
                           (answer, research); default en
  --safesearch <level>     off | moderate | strict (default moderate)

web flags:
  --count <n>              Results, 1-20 (default 10)
  --offset <n>             Page, 0-9 (default 0)
  --freshness <f>          pd | pw | pm | py | YYYY-MM-DDtoYYYY-MM-DD
  --extra-snippets         Up to five extra excerpts per result
  --full                   Pass every upstream field through (default: compact)

context flags:
  --count <n>              Results to analyse, 1-50 (default 10)
  --freshness <f>          As for web
  --max-tokens <n>         Context budget, 1024-32768 (default 8192)
  --max-urls <n>           URLs to draw from, 1-50 (default 20)

answer flags:
  --max-tokens <n>         Reply token cap (default: the API's)

research flags:
  --max-queries <n>        Searches per iteration, 1-50 (default 10)
  --max-iterations <n>     Iterations, 1-5 (default 2)
  --max-seconds <n>        Time budget, 1-300 (default 120)

Exit codes:
  0  the query completed (zero results is a valid answer)
  1  upstream failure, rate limit or timeout — no answer obtained
  2  error (invalid input, bad configuration, missing API key)

Every command spends one or more billed requests and prints what this call
cost, plus the rate budget left, after its result. Nothing is cached — the
Brave ToS permits only transient retention of results — so an identical call
is a second charge. Results are Brave's and their sources': cite them, do not
store or redistribute them, and never use them to train or evaluate a model.

research is the expensive command: up to max-queries searches per iteration,
max-iterations times, plus tokens. Its progress is printed to stderr while it
runs, because a silent wait of up to five minutes looks like a hang.

The API key is read from [api] api_key in the config file or from
BRAVE_SEARCH_API_KEY. It is never accepted as a flag.
`)
}
