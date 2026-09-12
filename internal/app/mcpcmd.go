package app

import (
	"context"
	"io"

	"github.com/nlink-jp/brave-search/internal/config"
	"github.com/nlink-jp/brave-search/internal/mcp"
)

// runMCP serves the stdio MCP server until stdin closes.
func runMCP(args []string, version string, stdin io.Reader, stdout, stderr io.Writer) int {
	var configPath string
	fs := newFlagSet("mcp", stderr)
	fs.StringVar(&configPath, "config", "", "config file path")
	fs.StringVar(&configPath, "c", "", "config file path (shorthand)")
	if _, err := parseInterleaved(fs, args); err != nil {
		return exitError
	}

	// The config is loaded here so a broken file fails at startup, where the
	// operator sees it, rather than on the first tool call. A missing key is
	// not a startup failure: the handshake and get_usage work without one, and
	// each tool reports missing_api_key when called.
	cfg, err := config.Load(configPath, 0)
	if err != nil {
		return fail(stderr, exitError, "%v", err)
	}

	srv := mcp.New(version, tools(cfg, version)...)
	if err := srv.Serve(context.Background(), stdin, stdout); err != nil {
		return fail(stderr, exitError, "mcp: %v", err)
	}
	return exitOK
}

// tools builds the tool set the server exposes. Empty in the scaffold; the
// Search and Answers units fill it in.
func tools(_ *config.Config, _ string) []mcp.Tool {
	return nil
}
