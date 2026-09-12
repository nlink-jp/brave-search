// Package engine is the shared core behind the CLI and the MCP server: it
// fills defaults from the configuration, validates every parameter against
// the documented ranges before a request is spent, calls the brave client,
// and shapes the response into the stable, compact schema both faces emit.
// Because both go through it, an agent and a human cannot get different
// answers to the same input.
package engine
