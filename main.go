// Command brave-search exposes three Brave Search API endpoints — Web Search
// (ranked results with snippets), LLM Context (pre-extracted page chunks for
// grounding) and Answers (answers grounded and cited by Brave, in a
// single-search mode and a multi-step research mode) — as a CLI and a local
// MCP server. It is a search primitive: it returns what Brave returns and
// never stores, recomposes or redistributes it.
package main

import (
	"os"

	"github.com/nlink-jp/brave-search/internal/app"
)

// version is set at build time via -ldflags "-X main.version=...".
var version = "dev"

func main() {
	os.Exit(app.Run(os.Args[1:], version))
}
